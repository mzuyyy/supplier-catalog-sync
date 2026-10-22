package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"catalogsync/internal/domain"
	"catalogsync/internal/store"
)

func buildCLI(t *testing.T) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(t.TempDir(), "catalogsync"+suffix)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-o", binary, "./cmd/catalogsync")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	return binary
}

func invoke(t *testing.T, binary string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("invoke CLI: %v\n%s", err, out)
	}
	return string(out), exit.ExitCode()
}

func TestHardKillThenNewCLIProcessResumesFromDisk(t *testing.T) {
	// Given: actual CLI, disk DB and a post-commit delay; no timing-based sleeps.
	binary := buildCLI(t)
	dbPath := filepath.Join(t.TempDir(), "catalog.db")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source := copyFixture(t, "basic")
	cmd := exec.CommandContext(ctx, binary, "--db", dbPath, "sync", "start", "--source", source, "--delay-between-batches", "3s")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			killErr := cmd.Process.Kill()
			if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				t.Logf("cleanup kill: %v", killErr)
			}
			if err := cmd.Wait(); err != nil {
				t.Logf("cleanup wait: %v", err)
			}
		}
	})
	ready := make(chan string, 1)
	scanned := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Job: ") && strings.Contains(line, "Batches committed: 2/3") {
				ready <- strings.Fields(line)[1]
			}
		}
		scanned <- scanner.Err()
	}()
	var id string
	select {
	case id = <-ready:
	case <-ctx.Done():
		t.Fatal("never observed second committed batch")
	}
	// When: unhandled force termination; replace the process, retaining SQLite file.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("force-killed process exited successfully")
	}
	waited = true
	if err := <-scanned; err != nil {
		t.Fatal(err)
	}
	// Then: separate processes observe incomplete work, then resume exactly once.
	out, code := invoke(t, binary, "--db", dbPath, "sync", "status", "--job", id)
	if code != 0 || !strings.Contains(out, "Status: RUNNING") || !strings.Contains(out, "Batches committed: 2/3") {
		t.Fatalf("after kill exit=%d output=%s", code, out)
	}
	out, code = invoke(t, binary, "--db", dbPath, "catalog", "list", "--include-withdrawn")
	if code != 0 || !strings.Contains(out, "product=P100 revision=3") {
		t.Fatalf("catalog after kill=%s", out)
	}
	out, code = invoke(t, binary, "--db", dbPath, "sync", "resume", "--job", id)
	if code != 0 || !strings.Contains(out, "Status: COMPLETED") {
		t.Fatalf("resume exit=%d output=%s", code, out)
	}
	s := database(t, dbPath)
	j, err := s.Job(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	products, err := s.Products(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != store.Completed || len(products) != 5 || len(changes) != 7 || j.Counts != (domain.Counts{Created: 4, Updated: 1, Withdrawn: 2, Duplicate: 2, Stale: 2}) {
		t.Fatalf("job=%+v products=%d changes=%d", j, len(products), len(changes))
	}
	out, code = invoke(t, binary, "--db", dbPath, "sync", "resume", "--job", id)
	if code != 0 || !strings.Contains(out, "COMPLETED") {
		t.Fatalf("completed resume exit=%d output=%s", code, out)
	}
	changes, err = s.Changes(context.Background(), id)
	if err != nil || len(changes) != 7 {
		t.Fatalf("duplicate history: %d err=%v", len(changes), err)
	}
}
