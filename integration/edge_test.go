package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

func TestEmptyFinalBatchIsValidCompletionEvidence(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	source := filepath.Join(t.TempDir(), "empty.json")
	body := `{"supplier_id":"S","update_id":"empty","declared_batch_count":1,"batches":[{"batch_index":0,"is_last":true,"events":[]}]}`
	if err := os.WriteFile(source, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	// When
	j, err := (&syncer.Service{Store: s}).Start(ctx, source, syncer.Options{})
	// Then
	if err != nil || j.Status != store.Completed || j.Next != 1 {
		t.Fatalf("job=%+v err=%v", j, err)
	}
	if err := s.CheckLedger(ctx, j); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeProductIdentitySeparatesSuppliers(t *testing.T) {
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
	body = []byte(strings.ReplaceAll(string(body), "demo-supplier", "other-supplier"))
	if err := os.WriteFile(source, body, 0600); err != nil {
		t.Fatal(err)
	}
	// When
	if _, err := svc.Start(ctx, source, syncer.Options{}); err != nil {
		t.Fatal(err)
	}
	// Then
	products, err := s.Products(ctx, true)
	if err != nil || len(products) != 10 {
		t.Fatalf("products=%d err=%v", len(products), err)
	}
}

func TestMalformedTypedEventIsReportedInItsBatch(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	source := copyFixture(t, "invalid")
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.ReplaceAll(string(body), `"revision":0`, `"revision":"bad"`))
	if err := os.WriteFile(source, body, 0600); err != nil {
		t.Fatal(err)
	}
	// When
	j, err := (&syncer.Service{Store: s}).Start(ctx, source, syncer.Options{})
	// Then
	if !errors.Is(err, syncer.ErrNeedsAttention) || j.Next != 2 {
		t.Fatalf("job=%+v err=%v", j, err)
	}
	issues, err := s.Issues(ctx, j.ID, false)
	if err != nil || len(issues) != 1 || issues[0].Code != "INVALID_RECORD" {
		t.Fatalf("issues=%+v err=%v", issues, err)
	}
}

func TestBadNewUpdatePreservesExistingCatalog(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	svc := syncer.Service{Store: s}
	if _, err := svc.Start(ctx, fixture("basic"), syncer.Options{}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Products(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	// When
	j, err := svc.Start(ctx, fixture("invalid"), syncer.Options{})
	// Then
	if !errors.Is(err, syncer.ErrNeedsAttention) || j.Next != 2 {
		t.Fatalf("job=%+v err=%v", j, err)
	}
	after, err := s.Products(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("bad update erased or modified existing catalog")
	}
}

func TestCancellationPersistsInterruptionAtSafeBoundary(t *testing.T) {
	// Given
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	svc := syncer.Service{Store: s}
	// When
	j, err := svc.Start(ctx, fixture("basic"), syncer.Options{Progress: func(j store.Job) error {
		if j.Next == 1 {
			cancel()
		}
		return nil
	}})
	// Then
	if !errors.Is(err, context.Canceled) || j.Status != store.Interrupted || j.Next != 1 {
		t.Fatalf("job=%+v err=%v", j, err)
	}
	final, err := svc.Resume(context.Background(), j.ID, syncer.Options{})
	if err != nil || final.Status != store.Completed {
		t.Fatalf("job=%+v err=%v", final, err)
	}
}
