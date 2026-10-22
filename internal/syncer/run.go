package syncer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"catalogsync/internal/domain"
	"catalogsync/internal/store"
	"catalogsync/internal/supplier"
)

func (s *Service) run(ctx context.Context, j store.Job, source *supplier.Fixture, o Options) (store.Job, error) {
	source.FailBeforeBatch = o.FailBeforeBatch
	if err := s.Store.CheckLedger(ctx, j); err != nil {
		return j, err
	}
	if err := s.Store.MarkRunning(ctx, j.ID); err != nil {
		return j, err
	}
	for {
		current, err := s.Store.Job(ctx, j.ID)
		if err != nil {
			return s.fail(ctx, j, store.Issue{Code: "STORAGE_ERROR", BatchIndex: &j.Next, Message: err.Error()}, err)
		}
		j = current
		if j.Status == store.Completed {
			return j, nil
		}
		if j.Next >= j.Total {
			return j, fmt.Errorf("%w: exhausted update without completion", store.ErrCorrupt)
		}
		b, err := source.Batch(ctx, j.Next)
		if err != nil {
			return s.batchFailure(ctx, j, err)
		}
		err = s.importBatch(ctx, j, b)
		if err != nil {
			var ambiguous *commitError
			if errors.As(err, &ambiguous) {
				recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				durable, reconcileErr := s.Store.Reconcile(recovery, j.ID)
				cancel()
				if reconcileErr != nil {
					return j, errors.Join(err, reconcileErr)
				}
				if durable.Next == j.Next+1 {
					j = durable
					if err := progress(o, j); err != nil {
						return s.batchFailure(ctx, j, err)
					}
					if j.Status == store.Completed {
						return j, nil
					}
					if err := delay(ctx, o.Delay); err != nil {
						return s.batchFailure(ctx, j, err)
					}
					continue
				}
				j = durable
			}
			return s.batchFailure(ctx, j, err)
		}
		j, err = s.Store.Job(ctx, j.ID)
		if err != nil {
			return s.batchFailure(ctx, current, err)
		}
		if err := progress(o, j); err != nil {
			return s.batchFailure(ctx, j, err)
		}
		if j.Status == store.Completed {
			return j, nil
		}
		if err := delay(ctx, o.Delay); err != nil {
			return s.batchFailure(ctx, j, err)
		}
	}
}

func (s *Service) batchFailure(ctx context.Context, j store.Job, err error) (store.Job, error) {
	issue := store.Issue{BatchIndex: &j.Next, Code: "STORAGE_ERROR", Message: err.Error()}
	switch {
	case errors.Is(err, domain.ErrConflict):
		issue.Code = "REVISION_CONFLICT"
		issue.Blocking = true
	case errors.Is(err, domain.ErrInvalid):
		issue.Code = "INVALID_RECORD"
		issue.Blocking = true
	case errors.Is(err, supplier.ErrInvalidManifest):
		issue.Code = "INVALID_MANIFEST"
		issue.Blocking = true
	case errors.Is(err, supplier.ErrUnavailable):
		issue.Code = "SUPPLIER_UNAVAILABLE"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		issue.Code = "INTERRUPTED"
	}
	var record *domain.RecordError
	if errors.As(err, &record) && record.ProductID != "" {
		issue.ProductID = &record.ProductID
	}
	return s.fail(ctx, j, issue, err)
}
func delay(ctx context.Context, d time.Duration) error {
	if d == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
