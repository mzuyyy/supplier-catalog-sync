package integration_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

func database(t *testing.T, path string) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func fixture(name string) string { return filepath.Join("..", "fixtures", "updates", name+".json") }

func TestOutageResumesWithNewConnection(t *testing.T) {
	// Given
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	s, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	service := syncer.Service{Store: s}
	j, err := service.Start(ctx, fixture("basic"), syncer.Options{FailBeforeBatch: 3})
	if !errors.Is(err, syncer.ErrInterrupted) || j.Status != store.Interrupted || j.Next != 2 {
		t.Fatalf("job=%+v err=%v", j, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := database(t, path)
	// When
	j, err = (&syncer.Service{Store: reopened}).Resume(ctx, j.ID, syncer.Options{})
	// Then
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != store.Completed || j.Next != 3 {
		t.Fatalf("job=%+v", j)
	}
	issues, err := reopened.Issues(ctx, j.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Status != "resolved" {
		t.Fatalf("issues=%+v", issues)
	}
}
