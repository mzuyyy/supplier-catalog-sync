package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"catalogsync/internal/domain"
	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

// The test binary doubles as a child holding a genuine uncommitted SQLite batch.
func TestOpenTransactionHelper(t *testing.T) {
	if os.Getenv("CATALOGSYNC_CRASH_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	s, err := store.Open(ctx, os.Getenv("CATALOGSYNC_TEST_DB"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	j, err := s.Job(ctx, os.Getenv("CATALOGSYNC_TEST_JOB"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRunning(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			t.Error(err)
		}
	}()
	event, err := domain.ParseEvent([]byte(`{"product_id":"P999","revision":1,"status":"active","name":"Uncommitted","price_cents":1}`))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := domain.Decide(nil, event)
	if err != nil {
		t.Fatal(err)
	}
	decision.Product.SupplierID = j.SupplierID
	if err := store.WriteEffect(ctx, tx, store.Effect{JobID: j.ID, BatchIndex: j.Next, Kind: decision.Kind, New: decision.Product}); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishBatch(ctx, tx, store.BatchCommit{Job: j, Index: j.Next, IsLast: true, Digest: "uncommitted-test-batch", Counts: domain.Counts{Created: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "uncommitted batch ready"); err != nil {
		t.Fatal(err)
	}
	// The parent owns stdin and keeps it open until this process is killed.
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func TestForceKillRollsBackOpenBatchIncludingCompletion(t *testing.T) {
	// Given
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "catalog.db")
	s, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	j, startErr := (&syncer.Service{Store: s}).Start(ctx, fixture("basic"), syncer.Options{FailBeforeBatch: 3})
	if startErr == nil || j.Next != 2 {
		t.Fatalf("setup=%+v err=%v", j, startErr)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestOpenTransactionHelper$")
	cmd.Env = append(os.Environ(), "CATALOGSYNC_CRASH_HELPER=1", "CATALOGSYNC_TEST_DB="+path, "CATALOGSYNC_TEST_JOB="+j.ID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stdin.Close(); err != nil {
			t.Logf("child stdin close: %v", err)
		}
	}()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("cleanup kill: %v", err)
			}
			if err := cmd.Wait(); err != nil {
				t.Logf("cleanup wait: %v", err)
			}
		}
	})
	ready := make(chan bool, 1)
	scanDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "uncommitted batch ready") {
				ready <- true
			}
		}
		scanDone <- scanner.Err()
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("child never reached open transaction barrier")
	}
	// When: kill after product, history, counters, ledger and COMPLETED writes,
	// before the single commit that would make them visible.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	scanErr := <-scanDone
	if err := cmd.Wait(); err == nil {
		t.Fatal("force kill returned success")
	}
	waited = true
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	// Then
	reopened := database(t, path)
	durable, err := reopened.Job(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if durable.Next != 2 || durable.Status != store.Running || durable.Counts != j.Counts {
		t.Fatalf("uncommitted checkpoint leaked: %+v", durable)
	}
	products, err := reopened.Products(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range products {
		if p.ProductID == "P999" {
			t.Fatal("uncommitted product survived kill")
		}
	}
	changes, err := reopened.Changes(ctx, j.ID)
	if err != nil || len(changes) != 6 {
		t.Fatalf("history=%d err=%v", len(changes), err)
	}
	if err := reopened.CheckLedger(ctx, durable); err != nil {
		t.Fatal(err)
	}
	final, err := (&syncer.Service{Store: reopened}).Resume(ctx, j.ID, syncer.Options{})
	if err != nil || final.Status != store.Completed {
		t.Fatalf("resume=%+v err=%v", final, err)
	}
}
