package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"catalogsync/internal/store"
	"catalogsync/internal/syncer"
)

const usage = `Usage: catalogsync [--db ./catalog.db] COMMAND
  sync start --source FILE [--fail-before-batch N] [--delay-between-batches 3s]
  sync resume --job ID [--fail-before-batch N] [--delay-between-batches 3s]
  sync status --job ID
  sync list
  sync changes --job ID
  sync issues --job ID [--all]
  catalog list [--include-withdrawn]
One synchronizer process at a time. Do not modify a source fixture after start.
`

type app struct {
	out   io.Writer
	store *store.Store
}

func Run(ctx context.Context, args []string, out, stderr io.Writer) int {
	err := execute(ctx, args, out)
	if err == nil {
		return 0
	}
	if _, writeErr := fmt.Fprintln(stderr, safeText(err.Error())); writeErr != nil {
		return 2
	}
	switch {
	case errors.Is(err, syncer.ErrInput), errors.Is(err, store.ErrNotFound):
		return 1
	case errors.Is(err, syncer.ErrNeedsAttention):
		return 3
	default:
		return 2
	}
}
func execute(ctx context.Context, args []string, out io.Writer) (err error) {
	root := flags("catalogsync")
	path := root.String("db", "./catalog.db", "SQLite file")
	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(out, usage)
			return err
		}
		return fmt.Errorf("%w: %w", syncer.ErrInput, err)
	}
	rest := root.Args()
	if len(rest) == 0 {
		return fmt.Errorf("%w: %s", syncer.ErrInput, usage)
	}
	if len(rest) == 1 && rest[0] == "help" {
		_, err = io.WriteString(out, usage)
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("%w: command and subcommand required\n%s", syncer.ErrInput, usage)
	}
	// Parse all command flags before opening/creating a database.
	request, err := parse(rest)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = io.WriteString(out, usage)
			return err
		}
		return err
	}
	s, err := store.Open(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	a := app{out: out, store: s}
	return a.dispatch(ctx, request)
}
func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

type request struct {
	command          string
	jobID            string
	source           string
	all              bool
	includeWithdrawn bool
	options          syncer.Options
}

func parse(args []string) (request, error) {
	r := request{command: args[0] + " " + args[1]}
	f := flags(r.command)
	switch r.command {
	case "sync start":
		f.StringVar(&r.source, "source", "", "immutable JSON source")
		f.IntVar(&r.options.FailBeforeBatch, "fail-before-batch", 0, "1-based batch to fail before")
		f.DurationVar(&r.options.Delay, "delay-between-batches", 0, "delay after committed nonfinal batches")
	case "sync resume":
		f.StringVar(&r.jobID, "job", "", "job ID")
		f.IntVar(&r.options.FailBeforeBatch, "fail-before-batch", 0, "1-based batch to fail before")
		f.DurationVar(&r.options.Delay, "delay-between-batches", 0, "delay after committed nonfinal batches")
	case "sync status", "sync changes", "sync issues":
		f.StringVar(&r.jobID, "job", "", "job ID")
		if r.command == "sync issues" {
			f.BoolVar(&r.all, "all", false, "include resolved issues")
		}
	case "sync list":
	case "catalog list":
		f.BoolVar(&r.includeWithdrawn, "include-withdrawn", false, "show tombstones")
	default:
		return r, fmt.Errorf("%w: unknown command %q", syncer.ErrInput, r.command)
	}
	if err := f.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return r, err
		}
		return r, fmt.Errorf("%w: %w", syncer.ErrInput, err)
	}
	if f.NArg() != 0 {
		return r, fmt.Errorf("%w: unexpected positional arguments", syncer.ErrInput)
	}
	switch r.command {
	case "sync start":
		if r.source == "" {
			return r, fmt.Errorf("%w: --source required", syncer.ErrInput)
		}
	case "sync resume", "sync status", "sync changes", "sync issues":
		if r.jobID == "" {
			return r, fmt.Errorf("%w: --job required", syncer.ErrInput)
		}
	}
	if r.options.FailBeforeBatch < 0 || r.options.Delay < 0 {
		return r, fmt.Errorf("%w: negative failure batch/delay", syncer.ErrInput)
	}
	return r, nil
}
