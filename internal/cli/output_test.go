package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"catalogsync/internal/cli"
	"catalogsync/internal/store"
)

func TestUntrustedIDsCannotInjectTerminalControlsOrLines(t *testing.T) {
	// Given: syntactically valid supplier strings include terminal commands/newlines.
	dir := t.TempDir()
	db := filepath.Join(dir, "catalog.db")
	source := filepath.Join(dir, "source.json")
	body := `{"supplier_id":"S\u001b[2J\nFORGED","update_id":"U\u001b[H\nFORGED","declared_batch_count":2,"batches":[{"batch_index":0,"is_last":false,"events":[{"product_id":"P\u001b[2J\nFORGED","revision":1,"status":"active","name":"Mouse","price_cents":1}]},{"batch_index":1,"is_last":true,"events":[{"product_id":"P\u001b[2J\nFORGED","revision":1,"status":"active","name":"Conflict","price_cents":1}]}]}`
	if err := os.WriteFile(source, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"--db", db, "sync", "start", "--source", source}, &out, &stderr)
	if code != 3 {
		t.Fatalf("setup exit=%d err=%s", code, stderr.String())
	}
	id := strings.Fields(out.String())[1]
	commands := [][]string{
		{"sync", "list"}, {"sync", "status", "--job", id},
		{"sync", "issues", "--job", id}, {"sync", "changes", "--job", id},
		{"catalog", "list", "--include-withdrawn"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			out.Reset()
			stderr.Reset()
			// When
			code := cli.Run(context.Background(), append([]string{"--db", db}, args...), &out, &stderr)
			// Then
			if code != 0 {
				t.Fatalf("exit=%d err=%s", code, stderr.String())
			}
			if strings.ContainsAny(out.String(), "\x1b\r") || strings.Contains(out.String(), "\nFORGED") {
				t.Fatalf("untrusted terminal output: %q", out.String())
			}
		})
	}
}

// A broken output pipe must not turn an interrupted start into success.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestOutputFailureReturnsNonSuccessAndLeavesResumableJob(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "catalog.db")
	var stderr bytes.Buffer
	// When
	code := cli.Run(context.Background(), []string{"--db", path, "sync", "start", "--source", "../../fixtures/updates/basic.json"}, brokenWriter{}, &stderr)
	// Then
	if code != 2 {
		t.Fatalf("exit=%d err=%s", code, stderr.String())
	}
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	jobs, err := s.Jobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != store.Interrupted || jobs[0].Next != 0 {
		t.Fatalf("jobs=%+v", jobs)
	}
}

func TestSourceArgumentCannotInjectTerminalControlsIntoErrors(t *testing.T) {
	// Given
	var out, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "catalog.db")
	// When
	code := cli.Run(context.Background(), []string{"--db", path, "sync", "start", "--source", "missing\x1b[2J\nFORGED.json"}, &out, &stderr)
	// Then
	if code != 2 {
		t.Fatalf("exit=%d", code)
	}
	if strings.ContainsAny(stderr.String(), "\x1b\r") || strings.Contains(stderr.String(), "\nFORGED") {
		t.Fatalf("raw controls in error: %q", stderr.String())
	}
}

func TestPersistedLastErrorCannotInjectTerminalControls(t *testing.T) {
	// Given
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	var out, stderr bytes.Buffer
	code := cli.Run(ctx, []string{"--db", path, "sync", "start", "--source", "../../fixtures/updates/basic.json", "--fail-before-batch", "1"}, &out, &stderr)
	if code != 2 {
		t.Fatalf("setup=%d %s", code, stderr.String())
	}
	id := strings.Fields(out.String())[1]
	s, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	saveErr := s.RecordFailure(ctx, store.Failure{JobID: id, State: store.Interrupted, Issue: store.Issue{Code: "STORAGE_ERROR", Message: "disk\x1b[2J\nFORGED"}})
	closeErr := s.Close()
	if saveErr != nil || closeErr != nil {
		t.Fatalf("save=%v close=%v", saveErr, closeErr)
	}
	out.Reset()
	stderr.Reset()
	// When
	code = cli.Run(ctx, []string{"--db", path, "sync", "status", "--job", id}, &out, &stderr)
	// Then
	if code != 0 {
		t.Fatalf("exit=%d %s", code, stderr.String())
	}
	if strings.ContainsAny(out.String(), "\x1b\r") || strings.Contains(out.String(), "\nFORGED") {
		t.Fatalf("raw controls in last error: %q", out.String())
	}
}
