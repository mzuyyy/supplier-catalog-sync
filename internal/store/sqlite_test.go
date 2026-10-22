package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"catalogsync/internal/store"
)

func TestSchemaSurvivesReopen(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "nested", "catalog.db")
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// When
	s, err = store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	// Then
	jobs, err := s.Jobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatal("new catalog contains jobs")
	}
}
