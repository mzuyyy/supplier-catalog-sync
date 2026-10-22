package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"catalogsync/internal/domain"
	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

func sqlDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestSQLFailureRollsBackEveryBatchEffect(t *testing.T) {
	tests := []struct{ name, ddl string }{
		{"product", `CREATE TRIGGER fail_test BEFORE INSERT ON products WHEN NEW.product_id='P103' BEGIN SELECT RAISE(ABORT,'injected product failure'); END`},
		{"history", `CREATE TRIGGER fail_test BEFORE INSERT ON sync_changes WHEN NEW.batch_index=1 BEGIN SELECT RAISE(ABORT,'injected audit failure'); END`},
		{"ledger", `CREATE TRIGGER fail_test BEFORE INSERT ON sync_batches WHEN NEW.batch_index=1 BEGIN SELECT RAISE(ABORT,'injected ledger failure'); END`},
		{"checkpoint and counters", `CREATE TRIGGER fail_test BEFORE UPDATE ON sync_jobs WHEN NEW.next_batch_index=2 BEGIN SELECT RAISE(ABORT,'injected checkpoint failure'); END`},
		{"deferred commit", `CREATE TABLE test_deferred(parent TEXT REFERENCES sync_jobs(job_id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER fail_test AFTER INSERT ON sync_batches WHEN NEW.batch_index=1 BEGIN INSERT INTO test_deferred VALUES('missing-job'); END`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "catalog.db")
			s := database(t, path)
			raw := sqlDB(t, path)
			opts := syncer.Options{Progress: func(j store.Job) error {
				if j.Next == 1 {
					_, err := raw.ExecContext(ctx, tt.ddl)
					return err
				}
				return nil
			}}
			// When
			j, err := (&syncer.Service{Store: s}).Start(ctx, fixture("basic"), opts)
			// Then
			if !errors.Is(err, syncer.ErrInterrupted) || j.Next != 1 || j.Status != store.Interrupted {
				t.Fatalf("job=%+v err=%v", j, err)
			}
			products, err := s.Products(ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(products) != 3 || products[0].Revision != 1 || products[1].Status != domain.Active {
				t.Fatalf("partial products: %+v", products)
			}
			changes, err := s.Changes(ctx, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != 3 || j.Counts != (domain.Counts{Created: 2, Withdrawn: 1}) {
				t.Fatalf("partial changes/counters: changes=%d counts=%+v", len(changes), j.Counts)
			}
			if err := s.CheckLedger(ctx, j); err != nil {
				t.Fatal(err)
			}
			var batches int
			if err := raw.QueryRowContext(ctx, "SELECT count(*) FROM sync_batches WHERE job_id=?", j.ID).Scan(&batches); err != nil {
				t.Fatal(err)
			}
			if batches != 1 {
				t.Fatalf("batch ledger=%d", batches)
			}
			if _, err := raw.ExecContext(ctx, "DROP TRIGGER fail_test"); err != nil {
				t.Fatal(err)
			}
			final, err := (&syncer.Service{Store: s}).Resume(ctx, j.ID, syncer.Options{})
			if err != nil || final.Status != store.Completed {
				t.Fatalf("resume=%+v err=%v", final, err)
			}
		})
	}
}

func TestCheckpointLedgerDisagreementBlocksResume(t *testing.T) {
	// Given
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := database(t, path)
	svc := syncer.Service{Store: s}
	j, err := svc.Start(ctx, fixture("basic"), syncer.Options{FailBeforeBatch: 3})
	if !errors.Is(err, syncer.ErrInterrupted) {
		t.Fatal(err)
	}
	raw := sqlDB(t, path)
	if _, err := raw.ExecContext(ctx, "DELETE FROM sync_batches WHERE job_id=? AND batch_index=0", j.ID); err != nil {
		t.Fatal(err)
	}
	// When
	after, err := svc.Resume(ctx, j.ID, syncer.Options{})
	// Then
	if !errors.Is(err, store.ErrCorrupt) || after.Next != 2 || after.Status == store.Completed {
		t.Fatalf("job=%+v err=%v", after, err)
	}
}

func TestCompletionCannotIgnoreOpenBlockingIssue(t *testing.T) {
	// Given
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := database(t, path)
	raw := sqlDB(t, path)
	opts := syncer.Options{Progress: func(j store.Job) error {
		if j.Next == 2 {
			_, err := raw.ExecContext(ctx, `INSERT INTO sync_issues(job_id,code,message,blocking,status,created_at) VALUES(?,'INVALID_RECORD','injected blocker',1,'open','now')`, j.ID)
			return err
		}
		return nil
	}}
	// When
	j, err := (&syncer.Service{Store: s}).Start(ctx, fixture("basic"), opts)
	// Then
	if err == nil || j.Status == store.Completed || j.Next != 2 {
		t.Fatalf("false complete: %+v %v", j, err)
	}
	var final int
	if err := raw.QueryRowContext(ctx, "SELECT count(*) FROM sync_batches WHERE job_id=? AND batch_index=2", j.ID).Scan(&final); err != nil {
		t.Fatal(err)
	}
	if final != 0 {
		t.Fatal("final batch persisted without completion proof")
	}
}
