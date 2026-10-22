package syncer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"catalogsync/internal/store"
	"catalogsync/internal/supplier"
)

var ErrInput = errors.New("invalid input")
var ErrInterrupted = errors.New("update interrupted; use sync resume --job JOB_ID")
var ErrNeedsAttention = errors.New("update needs attention; inspect sync issues --job JOB_ID")

type Options struct {
	FailBeforeBatch int
	Delay           time.Duration
	Progress        func(store.Job) error
}

type Service struct{ Store *store.Store }

func (s *Service) Start(ctx context.Context, path string, options Options) (store.Job, error) {
	source, err := supplier.Load(ctx, path)
	if err != nil {
		if errors.Is(err, supplier.ErrInvalidManifest) {
			return store.Job{}, fmt.Errorf("%w: %w", ErrInput, err)
		}
		return store.Job{}, fmt.Errorf("load update: %w", err)
	}
	if options.FailBeforeBatch < 0 || options.FailBeforeBatch > source.Info.DeclaredBatchCount || options.Delay < 0 {
		return store.Job{}, fmt.Errorf("%w: invalid failure batch or delay", ErrInput)
	}
	existing, err := s.Store.FindJob(ctx, source.Info)
	switch {
	case err == nil:
		if existing.SourceSHA256 != source.SHA256 || existing.Total != source.Info.DeclaredBatchCount {
			return existing, fmt.Errorf("%w: source identity collision for existing job %s", ErrInput, existing.ID)
		}
		if err := s.Store.CheckLedger(ctx, existing); err != nil {
			return existing, err
		}
		return existing, stateError(existing.Status)
	case !errors.Is(err, store.ErrNotFound):
		return store.Job{}, err
	}
	job, err := s.Store.CreateJob(ctx, source)
	if err != nil {
		return job, err
	}
	if err := progress(options, job); err != nil {
		return s.fail(ctx, job, store.Issue{Code: "STORAGE_ERROR", Message: err.Error(), BatchIndex: &job.Next}, err)
	}
	return s.run(ctx, job, source, options)
}

func (s *Service) Resume(ctx context.Context, id string, options Options) (store.Job, error) {
	job, err := s.Store.Job(ctx, id)
	if err != nil {
		return job, err
	}
	if err := s.Store.CheckLedger(ctx, job); err != nil {
		return job, err
	}
	switch job.Status {
	case store.Completed:
		return job, nil
	case store.NeedsAttention:
		return job, ErrNeedsAttention
	case store.Pending, store.Running, store.Interrupted:
	default:
		return job, fmt.Errorf("unknown job state %s", job.Status)
	}
	if options.FailBeforeBatch < 0 || options.FailBeforeBatch > job.Total || options.Delay < 0 {
		return job, fmt.Errorf("%w: invalid failure batch or delay", ErrInput)
	}
	source, err := supplier.Load(ctx, job.SourcePath)
	if err != nil {
		issue := store.Issue{Code: "SUPPLIER_UNAVAILABLE", Message: err.Error(), BatchIndex: &job.Next}
		if errors.Is(err, supplier.ErrInvalidManifest) {
			issue.Code = "SOURCE_CHANGED"
			issue.Blocking = true
		}
		return s.fail(ctx, job, issue, err)
	}
	if source.SHA256 != job.SourceSHA256 || source.Info.SupplierID != job.SupplierID || source.Info.UpdateID != job.UpdateID || source.Info.DeclaredBatchCount != job.Total {
		err := errors.New("source changed; immutable fixture cannot be replaced; supply a corrected new update")
		return s.fail(ctx, job, store.Issue{Code: "SOURCE_CHANGED", Message: err.Error(), Blocking: true}, err)
	}
	return s.run(ctx, job, source, options)
}

func progress(o Options, j store.Job) error {
	if o.Progress == nil {
		return nil
	}
	if err := o.Progress(j); err != nil {
		return fmt.Errorf("write progress: %w", err)
	}
	return nil
}
func stateError(state store.State) error {
	switch state {
	case store.Completed:
		return nil
	case store.NeedsAttention:
		return ErrNeedsAttention
	case store.Pending, store.Running, store.Interrupted:
		return ErrInterrupted
	default:
		return fmt.Errorf("unknown job state %s", state)
	}
}
func (s *Service) fail(ctx context.Context, j store.Job, issue store.Issue, cause error) (store.Job, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	state := store.Interrupted
	classified := ErrInterrupted
	if issue.Blocking {
		state = store.NeedsAttention
		classified = ErrNeedsAttention
	}
	saveErr := s.Store.RecordFailure(cleanup, store.Failure{JobID: j.ID, State: state, Issue: issue})
	latest, readErr := s.Store.Job(cleanup, j.ID)
	if readErr == nil {
		j = latest
	}
	return j, errors.Join(classified, cause, saveErr, readErr)
}
