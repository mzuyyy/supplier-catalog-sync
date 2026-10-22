package syncer

import (
	"context"
	"encoding/json"
	"fmt"

	"catalogsync/internal/domain"
	"catalogsync/internal/store"
	"catalogsync/internal/supplier"
)

type commitError struct{ cause error }

func (e *commitError) Error() string { return "batch commit: " + e.cause.Error() }
func (e *commitError) Unwrap() error { return e.cause }

func (s *Service) importBatch(ctx context.Context, job store.Job, b supplier.Batch) (err error) {
	tx, err := s.Store.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin batch: %w", err)
	}
	defer store.Rollback(tx, &err)
	current, err := store.JobTx(ctx, tx, job.ID)
	if err != nil {
		return err
	}
	if current.Next != b.Index || current.Status != store.Running {
		return fmt.Errorf("%w: batch %d does not match running checkpoint", store.ErrCorrupt, b.Index)
	}
	if err := store.CheckLedgerTx(ctx, tx, current); err != nil {
		return err
	}
	if b.IsLast != (b.Index == current.Total-1) {
		return fmt.Errorf("%w: incorrect batch end marker", supplier.ErrInvalidManifest)
	}
	var counts domain.Counts
	for _, raw := range b.Events {
		event, err := domain.ParseEvent(raw)
		if err != nil {
			return err
		}
		old, err := store.ProductTx(ctx, tx, store.ProductKey{SupplierID: job.SupplierID, ProductID: event.ProductID()})
		if err != nil {
			return err
		}
		decision, err := domain.Decide(old, event)
		if err != nil {
			return err
		}
		if err := counts.Add(decision.Kind); err != nil {
			return err
		}
		switch decision.Kind {
		case domain.Stale, domain.Duplicate:
		case domain.Created, domain.Updated, domain.Withdrew, domain.Reactivated:
			decision.Product.SupplierID = job.SupplierID
			effect := store.Effect{JobID: job.ID, BatchIndex: b.Index, Kind: decision.Kind, Old: old, New: decision.Product}
			if err := store.WriteEffect(ctx, tx, effect); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown reducer decision %q", decision.Kind)
		}
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("batch digest: %w", err)
	}
	if err := store.FinishBatch(ctx, tx, store.BatchCommit{Job: current, Index: b.Index, IsLast: b.IsLast, Digest: supplier.Digest(raw), Counts: counts}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return &commitError{cause: err}
	}
	return nil
}
