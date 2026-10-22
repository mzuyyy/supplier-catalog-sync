package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

func TestChangedSourceBlocksResumeButCollisionPreservesOriginalJob(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	svc := syncer.Service{Store: s}
	source := copyFixture(t, "basic")
	j, err := svc.Start(ctx, source, syncer.Options{FailBeforeBatch: 3})
	if !errors.Is(err, syncer.ErrInterrupted) {
		t.Fatal(err)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, append(body, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	// When
	collision, err := svc.Start(ctx, source, syncer.Options{})
	// Then
	if !errors.Is(err, syncer.ErrInput) || collision.ID != j.ID || collision.Status != store.Interrupted {
		t.Fatalf("collision=%+v err=%v", collision, err)
	}
	blocked, err := svc.Resume(ctx, j.ID, syncer.Options{})
	if !errors.Is(err, syncer.ErrNeedsAttention) || blocked.Status != store.NeedsAttention || blocked.Next != 2 {
		t.Fatalf("job=%+v err=%v", blocked, err)
	}
	issues, err := s.Issues(ctx, j.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range issues {
		if i.Code == "SOURCE_CHANGED" && i.Blocking {
			found = true
		}
	}
	if !found {
		t.Fatal("missing source changed issue")
	}
}

func TestRepeatedOutageDeduplicatesIssueAndStartDoesNotResume(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	svc := syncer.Service{Store: s}
	original, err := svc.Start(ctx, fixture("basic"), syncer.Options{FailBeforeBatch: 3})
	if !errors.Is(err, syncer.ErrInterrupted) {
		t.Fatal(err)
	}
	// When
	repeated, err := svc.Start(ctx, fixture("basic"), syncer.Options{})
	// Then
	if !errors.Is(err, syncer.ErrInterrupted) || repeated.Next != 2 || repeated.Counts != original.Counts {
		t.Fatalf("start resumed unexpectedly: %+v %v", repeated, err)
	}
	repeated, err = svc.Resume(ctx, original.ID, syncer.Options{FailBeforeBatch: 3})
	if !errors.Is(err, syncer.ErrInterrupted) || repeated.Counts != original.Counts {
		t.Fatalf("retry=%+v err=%v", repeated, err)
	}
	issues, err := s.Issues(ctx, original.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Status != "open" {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestMissingSourceCanBeRestoredAndResumed(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	svc := syncer.Service{Store: s}
	path := copyFixture(t, "basic")
	j, err := svc.Start(ctx, path, syncer.Options{FailBeforeBatch: 3})
	if !errors.Is(err, syncer.ErrInterrupted) {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// When
	unavailable, err := svc.Resume(ctx, j.ID, syncer.Options{})
	// Then
	if !errors.Is(err, syncer.ErrInterrupted) || unavailable.Next != 2 {
		t.Fatalf("job=%+v err=%v", unavailable, err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Resume(ctx, j.ID, syncer.Options{})
	if err != nil || final.Status != store.Completed {
		t.Fatalf("final=%+v err=%v", final, err)
	}
}

func TestInvalidManifestNeverCreatesCompletedJob(t *testing.T) {
	for _, body := range []string{
		`{"supplier_id":"S","update_id":"U","declared_batch_count":0,"batches":[]}`,
		`{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"batch_index":0,"is_last":false,"events":[]}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			// Given
			ctx := context.Background()
			s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
			source := filepath.Join(t.TempDir(), "bad.json")
			if err := os.WriteFile(source, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			// When
			_, err := (&syncer.Service{Store: s}).Start(ctx, source, syncer.Options{})
			// Then
			if !errors.Is(err, syncer.ErrInput) {
				t.Fatalf("err=%v", err)
			}
			jobs, err := s.Jobs(ctx)
			if err != nil || len(jobs) != 0 {
				t.Fatalf("jobs=%+v err=%v", jobs, err)
			}
		})
	}
}

func TestDifferentUpdateReplaysKnownRevisionsWithoutNewChanges(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	svc := syncer.Service{Store: s}
	if _, err := svc.Start(ctx, fixture("basic"), syncer.Options{}); err != nil {
		t.Fatal(err)
	}
	source := copyFixture(t, "basic")
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.ReplaceAll(string(body), "basic-001", "basic-002"))
	if err := os.WriteFile(source, body, 0600); err != nil {
		t.Fatal(err)
	}
	// When
	j, err := svc.Start(ctx, source, syncer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Then
	changes, err := s.Changes(ctx, j.ID)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	if j.Counts.Duplicate != 7 || j.Counts.Stale != 4 {
		t.Fatalf("counts=%+v", j.Counts)
	}
}
