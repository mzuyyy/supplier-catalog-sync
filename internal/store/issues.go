package store

import (
	"context"
	"errors"
	"fmt"
)

type Issue struct {
	ID         int64   `json:"id"`
	JobID      string  `json:"job_id"`
	BatchIndex *int    `json:"batch_index"`
	ProductID  *string `json:"product_id"`
	Code       string  `json:"code"`
	Message    string  `json:"message"`
	Blocking   bool    `json:"blocking"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	ResolvedAt *string `json:"resolved_at"`
}

type Failure struct {
	JobID string
	State State
	Issue Issue
}

func (s *Store) RecordFailure(ctx context.Context, f Failure) (err error) {
	tx, err := s.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failure transaction: %w", err)
	}
	defer Rollback(tx, &err)
	j, err := JobTx(ctx, tx, f.JobID)
	if err != nil {
		return err
	}
	if j.Status == Completed {
		return nil
	}
	i := f.Issue
	now := timestamp()
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_issues(job_id,batch_index,product_id,code,message,blocking,status,created_at) VALUES(?,?,?,?,?,?,'open',?) ON CONFLICT DO NOTHING`, f.JobID, i.BatchIndex, i.ProductID, i.Code, i.Message, i.Blocking, now)
	if err != nil {
		return fmt.Errorf("record issue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sync_jobs SET status=?,last_error=?,updated_at=? WHERE job_id=?", f.State, i.Message, now, f.JobID); err != nil {
		return fmt.Errorf("record failed state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failure state: %w", err)
	}
	return nil
}
func (s *Store) Issues(ctx context.Context, id string, all bool) (issues []Issue, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,batch_index,product_id,code,message,blocking,status,created_at,resolved_at FROM sync_issues WHERE job_id=? AND (status='open' OR ?) ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END,id`, id, all)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	issues = []Issue{}
	for rows.Next() {
		var i Issue
		if err := rows.Scan(&i.ID, &i.JobID, &i.BatchIndex, &i.ProductID, &i.Code, &i.Message, &i.Blocking, &i.Status, &i.CreatedAt, &i.ResolvedAt); err != nil {
			return nil, fmt.Errorf("read issue: %w", err)
		}
		issues = append(issues, i)
	}
	return issues, rows.Err()
}

type Change struct {
	ID         int64  `json:"id"`
	BatchIndex int    `json:"batch_index"`
	SupplierID string `json:"supplier_id"`
	ProductID  string `json:"product_id"`
	Revision   int64  `json:"revision"`
	Type       string `json:"change_type"`
	OldJSON    string `json:"old_record_json"`
	NewJSON    string `json:"new_record_json"`
	CreatedAt  string `json:"created_at"`
}

func (s *Store) Changes(ctx context.Context, id string) (changes []Change, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,batch_index,supplier_id,product_id,revision,change_type,old_record_json,new_record_json,created_at FROM sync_changes WHERE job_id=? ORDER BY batch_index,id`, id)
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	changes = []Change{}
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.ID, &c.BatchIndex, &c.SupplierID, &c.ProductID, &c.Revision, &c.Type, &c.OldJSON, &c.NewJSON, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("read change: %w", err)
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}
