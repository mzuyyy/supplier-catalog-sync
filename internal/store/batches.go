package store

import (
	"context"
	"database/sql"
	"fmt"

	"catalogsync/internal/domain"
)

func CheckLedgerTx(ctx context.Context, tx *sql.Tx, j Job) error {
	var count int
	var minimum, maximum sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT count(*),min(batch_index),max(batch_index) FROM sync_batches WHERE job_id=?", j.ID).Scan(&count, &minimum, &maximum); err != nil {
		return fmt.Errorf("check batch ledger: %w", err)
	}
	if count != j.Next || (count > 0 && (minimum.Int64 != 0 || maximum.Int64 != int64(j.Next-1))) {
		return fmt.Errorf("%w: checkpoint=%d rows=%d range=%v..%v", ErrCorrupt, j.Next, count, minimum, maximum)
	}
	if j.Status == Completed && j.Next != j.Total {
		return fmt.Errorf("%w: completed job has missing batches", ErrCorrupt)
	}
	return nil
}
func (s *Store) CheckLedger(ctx context.Context, j Job) (err error) {
	tx, err := s.Begin(ctx)
	if err != nil {
		return err
	}
	defer Rollback(tx, &err)
	return CheckLedgerTx(ctx, tx, j)
}

type BatchCommit struct {
	Job    Job
	Index  int
	IsLast bool
	Digest string
	Counts domain.Counts
}

func FinishBatch(ctx context.Context, tx *sql.Tx, b BatchCommit) error {
	j := b.Job
	now := timestamp()
	if b.Index != j.Next || b.IsLast != (b.Index == j.Total-1) {
		return fmt.Errorf("invalid batch boundary %d", b.Index)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO sync_batches(job_id,batch_index,batch_digest,committed_at) VALUES(?,?,?,?)", j.ID, b.Index, b.Digest, now); err != nil {
		return fmt.Errorf("write batch ledger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sync_issues SET status='resolved',resolved_at=? WHERE job_id=? AND batch_index=? AND blocking=0 AND status='open'", now, j.ID, b.Index); err != nil {
		return fmt.Errorf("resolve outage: %w", err)
	}
	state := Running
	var finished *string
	if b.IsLast {
		j.Next = b.Index + 1
		if err := CheckLedgerTx(ctx, tx, j); err != nil {
			return err
		}
		var blocking int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sync_issues WHERE job_id=? AND blocking=1 AND status='open'", j.ID).Scan(&blocking); err != nil {
			return fmt.Errorf("completion issues: %w", err)
		}
		if blocking > 0 {
			return fmt.Errorf("completion blocked by %d open issues", blocking)
		}
		state = Completed
		finished = &now
	}
	c := b.Counts
	res, err := tx.ExecContext(ctx, `UPDATE sync_jobs SET next_batch_index=?,status=?,created_count=created_count+?,updated_count=updated_count+?,withdrawn_count=withdrawn_count+?,reactivated_count=reactivated_count+?,duplicate_count=duplicate_count+?,stale_count=stale_count+?,finished_at=?,updated_at=?,last_error=NULL WHERE job_id=? AND next_batch_index=? AND status='RUNNING'`, b.Index+1, state, c.Created, c.Updated, c.Withdrawn, c.Reactivated, c.Duplicate, c.Stale, finished, now, j.ID, b.Index)
	if err != nil {
		return fmt.Errorf("write checkpoint/counters: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checkpoint rows: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("%w: checkpoint moved during batch", ErrCorrupt)
	}
	return nil
}
