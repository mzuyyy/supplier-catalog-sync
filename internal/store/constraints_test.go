package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestForeignKeysAndConstraintsHoldOnReplacementConnections(t *testing.T) {
	// Given
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "catalog # dữ liệu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	s.db.SetMaxIdleConns(0)
	// When / Then: every query now acquires a freshly configured connection.
	for range 2 {
		var enabled int
		if err := s.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
			t.Fatalf("foreign_keys=%d err=%v", enabled, err)
		}
		if _, err := s.db.ExecContext(ctx, "INSERT INTO sync_batches VALUES('missing',0,'digest','now')"); err == nil {
			t.Fatal("foreign key violation accepted")
		}
		if _, err := s.db.ExecContext(ctx, "INSERT INTO products VALUES('S','P',0,'withdrawn',NULL,NULL,'digest','now')"); err == nil {
			t.Fatal("invalid revision accepted")
		}
	}
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal=%s err=%v", mode, err)
	}
	var synchronous int
	if err := s.db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil || synchronous != 2 {
		t.Fatalf("synchronous=%d err=%v", synchronous, err)
	}
	var busy int
	if err := s.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil || busy != 5000 {
		t.Fatalf("busy_timeout=%d err=%v", busy, err)
	}
}
