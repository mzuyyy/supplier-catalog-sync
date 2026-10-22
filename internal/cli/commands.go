package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

func (a app) print(format string, args ...any) error {
	_, err := fmt.Fprintf(a.out, format, args...)
	return err
}

func (a app) dispatch(ctx context.Context, r request) error {
	switch r.command {
	case "sync start", "sync resume":
		return a.synchronize(ctx, r)
	case "sync status":
		j, err := a.store.Job(ctx, r.jobID)
		if err != nil {
			return err
		}
		return a.status(ctx, j)
	case "sync list":
		jobs, err := a.store.Jobs(ctx)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if err := a.print("%s supplier=%s update=%s status=%s batches=%d/%d updated=%s\n", j.ID, safeText(j.SupplierID), safeText(j.UpdateID), j.Status, j.Next, j.Total, j.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	case "catalog list":
		products, err := a.store.Products(ctx, r.includeWithdrawn)
		if err != nil {
			return err
		}
		for _, p := range products {
			name, price := "", ""
			if p.Name != nil {
				name = *p.Name
			}
			if p.PriceCents != nil {
				price = fmt.Sprint(*p.PriceCents)
			}
			if err := a.print("supplier=%s product=%s revision=%d status=%s name=%q price_cents=%s\n", safeText(p.SupplierID), safeText(p.ProductID), p.Revision, p.Status, name, price); err != nil {
				return err
			}
		}
		return nil
	case "sync changes", "sync issues":
		if _, err := a.store.Job(ctx, r.jobID); err != nil {
			return err
		}
		if r.command == "sync changes" {
			return a.changes(ctx, r.jobID)
		}
		return a.issues(ctx, r)
	default:
		return fmt.Errorf("%w: unsupported command", syncer.ErrInput)
	}
}
func (a app) synchronize(ctx context.Context, r request) error {
	service := syncer.Service{Store: a.store}
	created := false
	r.options.Progress = func(j store.Job) error {
		if j.Status == store.Pending {
			created = true
		}
		return a.print("Job: %s | Status: %s | Batches committed: %d/%d\n", j.ID, j.Status, j.Next, j.Total)
	}
	var j store.Job
	var err error
	if r.command == "sync start" {
		j, err = service.Start(ctx, r.source, r.options)
	} else {
		j, err = service.Resume(ctx, r.jobID, r.options)
	}
	if j.ID != "" {
		if r.command == "sync start" && !created && !errors.Is(err, syncer.ErrInput) {
			message := "Existing job; use sync resume --job " + j.ID
			if j.Status == store.Completed {
				message = "Update already imported; existing job reused"
			}
			err = errors.Join(err, a.print("%s\n", message))
		}
		err = errors.Join(err, a.status(context.WithoutCancel(ctx), j))
	}
	return err
}
func (a app) status(ctx context.Context, j store.Job) error {
	issues, err := a.store.Issues(ctx, j.ID, false)
	if err != nil {
		return err
	}
	blocking := 0
	for _, i := range issues {
		if i.Blocking {
			blocking++
		}
	}
	c := j.Counts
	if err := a.print("Job: %s\nSupplier: %s\nUpdate: %s\nStatus: %s\nBatches committed: %d/%d\nChanges: created=%d updated=%d withdrawn=%d reactivated=%d\nSkipped: duplicate=%d stale=%d\nOpen blocking issues: %d | Other open issues: %d\n", j.ID, safeText(j.SupplierID), safeText(j.UpdateID), j.Status, j.Next, j.Total, c.Created, c.Updated, c.Withdrawn, c.Reactivated, c.Duplicate, c.Stale, blocking, len(issues)-blocking); err != nil {
		return err
	}
	if j.LastError != nil {
		if err := a.print("Last error: %s\n", safeText(*j.LastError)); err != nil {
			return err
		}
	}
	switch j.Status {
	case store.Interrupted, store.Running, store.Pending:
		return a.print("Next action: sync resume --job %s (ensure previous synchronizer has stopped)\n", j.ID)
	case store.NeedsAttention:
		return a.print("Next action: sync issues --job %s; supply a corrected new update; keep original source immutable\n", j.ID)
	case store.Completed:
		return nil
	default:
		return fmt.Errorf("unknown job state %s", j.Status)
	}
}
func (a app) changes(ctx context.Context, id string) error {
	changes, err := a.store.Changes(ctx, id)
	if err != nil {
		return err
	}
	for _, c := range changes {
		if err := a.print("change=%d batch=%d type=%s supplier=%s product=%s revision=%d\n  old=%s\n  new=%s\n", c.ID, c.BatchIndex+1, c.Type, safeText(c.SupplierID), safeText(c.ProductID), c.Revision, c.OldJSON, c.NewJSON); err != nil {
			return err
		}
	}
	return nil
}
func (a app) issues(ctx context.Context, r request) error {
	issues, err := a.store.Issues(ctx, r.jobID, r.all)
	if err != nil {
		return err
	}
	for _, i := range issues {
		batch, product := "-", "-"
		if i.BatchIndex != nil {
			batch = fmt.Sprint(*i.BatchIndex + 1)
		}
		if i.ProductID != nil {
			product = safeText(*i.ProductID)
		}
		retry := "yes"
		if i.Blocking {
			retry = "no; corrected new update required"
		}
		if err := a.print("issue=%d status=%s blocking=%t code=%s batch=%s product=%s retry=%s message=%q\n", i.ID, i.Status, i.Blocking, i.Code, batch, product, retry, i.Message); err != nil {
			return err
		}
	}
	return nil
}

// safeText preserves readable Unicode while rendering terminal controls as text.
func safeText(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}
