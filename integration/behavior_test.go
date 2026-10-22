package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"catalogsync/internal/domain"
	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

func TestCompletedReplayHasNoNewDatabaseEffects(t *testing.T) {
	// Given
	ctx := context.Background()
	s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
	svc := syncer.Service{Store: s}
	original, err := svc.Start(ctx, fixture("basic"), syncer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Changes(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	// When
	replay, err := svc.Start(ctx, fixture("basic"), syncer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Then
	after, err := s.Changes(ctx, replay.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, replay) || !reflect.DeepEqual(before, after) {
		t.Fatal("replay changed job or history")
	}
	products, err := s.Products(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 5 || len(after) != 7 {
		t.Fatalf("products=%d changes=%d", len(products), len(after))
	}
	want := domain.Counts{Created: 4, Updated: 1, Withdrawn: 2, Duplicate: 2, Stale: 2}
	if replay.Counts != want {
		t.Fatalf("counts=%+v want=%+v", replay.Counts, want)
	}
}

func TestInvalidAndConflictingBatchesRollBack(t *testing.T) {
	tests := []struct {
		name, code              string
		next, products, changes int
	}{
		{"conflict", "REVISION_CONFLICT", 1, 1, 1},
		{"invalid", "INVALID_RECORD", 2, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "catalog.db")
			s, err := store.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			svc := syncer.Service{Store: s}
			// When
			j, err := svc.Start(ctx, fixture(tt.name), syncer.Options{})
			// Then
			if !errors.Is(err, syncer.ErrNeedsAttention) || j.Status != store.NeedsAttention || j.Next != tt.next {
				t.Fatalf("job=%+v err=%v", j, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			fresh := database(t, path)
			products, err := fresh.Products(ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			changes, err := fresh.Changes(ctx, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(products) != tt.products || len(changes) != tt.changes || j.Counts.Created != int64(tt.products) {
				t.Fatalf("partial batch persisted: products=%+v changes=%+v counts=%+v", products, changes, j.Counts)
			}
			issues, err := fresh.Issues(ctx, j.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(issues) != 1 || issues[0].Code != tt.code || !issues[0].Blocking || issues[0].BatchIndex == nil || *issues[0].BatchIndex != tt.next || issues[0].ProductID == nil {
				t.Fatalf("issues=%+v", issues)
			}
			if _, err := (&syncer.Service{Store: fresh}).Resume(ctx, j.ID, syncer.Options{}); !errors.Is(err, syncer.ErrNeedsAttention) {
				t.Fatalf("blocked resume err=%v", err)
			}
		})
	}
}

func TestOrderingAndExplicitReactivation(t *testing.T) {
	for _, name := range []string{"out_of_order", "reactivate"} {
		t.Run(name, func(t *testing.T) {
			// Given
			ctx := context.Background()
			s := database(t, filepath.Join(t.TempDir(), "catalog.db"))
			// When
			j, err := (&syncer.Service{Store: s}).Start(ctx, fixture(name), syncer.Options{})
			if err != nil {
				t.Fatal(err)
			}
			// Then
			products, err := s.Products(ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			if name == "reactivate" {
				if len(products) != 1 || products[0].Revision != 6 || products[0].Status != domain.Active || j.Counts.Reactivated != 1 {
					t.Fatalf("products=%+v counts=%+v", products, j.Counts)
				}
			} else {
				if len(products) != 2 || products[0].Revision != 3 || products[1].Status != domain.Withdrawn || products[1].Revision != 5 || products[1].Name != nil || j.Counts.Stale != 3 {
					t.Fatalf("products=%+v counts=%+v", products, j.Counts)
				}
				active, err := s.Products(ctx, false)
				if err != nil || len(active) != 1 {
					t.Fatalf("active=%+v err=%v", active, err)
				}
			}
		})
	}
}

func copyFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
