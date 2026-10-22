package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"catalogsync/internal/domain"
	"catalogsync/internal/supplier"
)

type State string

const (
	Pending        State = "PENDING"
	Running        State = "RUNNING"
	Interrupted    State = "INTERRUPTED"
	NeedsAttention State = "NEEDS_ATTENTION"
	Completed      State = "COMPLETED"
)

type Job struct {
	ID           string        `json:"job_id"`
	SupplierID   string        `json:"supplier_id"`
	UpdateID     string        `json:"update_id"`
	SourcePath   string        `json:"source_path"`
	SourceSHA256 string        `json:"source_sha256"`
	Total        int           `json:"declared_batch_count"`
	Next         int           `json:"next_batch_index"`
	Status       State         `json:"status"`
	Counts       domain.Counts `json:"counts"`
	StartedAt    string        `json:"started_at"`
	FinishedAt   *string       `json:"finished_at"`
	UpdatedAt    string        `json:"updated_at"`
	LastError    *string       `json:"last_error"`
}

const jobColumns = `job_id,supplier_id,update_id,source_path,source_sha256,declared_batch_count,next_batch_index,status,created_count,updated_count,withdrawn_count,reactivated_count,duplicate_count,stale_count,started_at,finished_at,updated_at,last_error`

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.SupplierID, &j.UpdateID, &j.SourcePath, &j.SourceSHA256, &j.Total, &j.Next, &j.Status, &j.Counts.Created, &j.Counts.Updated, &j.Counts.Withdrawn, &j.Counts.Reactivated, &j.Counts.Duplicate, &j.Counts.Stale, &j.StartedAt, &j.FinishedAt, &j.UpdatedAt, &j.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("read job: %w", err)
	}
	return j, nil
}

func (s *Store) Job(ctx context.Context, id string) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM sync_jobs WHERE job_id=?", id))
}
func JobTx(ctx context.Context, tx *sql.Tx, id string) (Job, error) {
	return scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM sync_jobs WHERE job_id=?", id))
}
func (s *Store) FindJob(ctx context.Context, m supplier.Manifest) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM sync_jobs WHERE supplier_id=? AND update_id=?", m.SupplierID, m.UpdateID))
}
func (s *Store) CreateJob(ctx context.Context, f *supplier.Fixture) (Job, error) {
	id := "JOB-" + rand.Text()
	now := timestamp()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_jobs(job_id,supplier_id,update_id,source_path,source_sha256,declared_batch_count,status,started_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, f.Info.SupplierID, f.Info.UpdateID, f.Path, f.SHA256, f.Info.DeclaredBatchCount, Pending, now, now)
	if err != nil {
		return Job{}, fmt.Errorf("create job: %w", err)
	}
	return s.Job(ctx, id)
}
func (s *Store) Jobs(ctx context.Context) (jobs []Job, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+jobColumns+" FROM sync_jobs ORDER BY started_at,job_id")
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	jobs = []Job{}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
func (s *Store) MarkRunning(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE sync_jobs SET status=?,last_error=NULL,updated_at=? WHERE job_id=? AND status IN ('PENDING','RUNNING','INTERRUPTED')", Running, timestamp(), id)
	if err != nil {
		return fmt.Errorf("mark running: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("running rows: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("job %s cannot run from current state", id)
	}
	return nil
}
func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
