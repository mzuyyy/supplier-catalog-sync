package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

var ErrCorrupt = errors.New("checkpoint/batch ledger corruption")
var ErrNotFound = errors.New("job not found")

type Store struct {
	db   *sql.DB
	path string
}

func Open(ctx context.Context, path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("DB path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return nil, fmt.Errorf("DB directory: %w", err)
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	query := url.Values{}
	for _, p := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"} {
		query.Add("_pragma", p)
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: abs}
	if err := s.migrate(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return s, nil
}

func (s *Store) Close() error                               { return s.db.Close() }
func (s *Store) Begin(ctx context.Context) (*sql.Tx, error) { return s.db.BeginTx(ctx, nil) }

func Rollback(tx *sql.Tx, result *error) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		*result = errors.Join(*result, fmt.Errorf("rollback: %w", err))
	}
}

func (s *Store) migrate(ctx context.Context) (err error) {
	tx, err := s.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migration begin: %w", err)
	}
	defer Rollback(tx, &err)
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("schema version: %w", err)
	}
	switch version {
	case 0:
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version=1"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	case 1:
	default:
		return fmt.Errorf("unsupported schema version %d", version)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration commit: %w", err)
	}
	return nil
}

// Reconcile reads authoritative durable state through a new pool after Commit errors.
func (s *Store) Reconcile(ctx context.Context, id string) (job Job, err error) {
	reopened, err := Open(ctx, s.path)
	if err != nil {
		return Job{}, fmt.Errorf("reopen after commit error: %w", err)
	}
	defer func() { err = errors.Join(err, reopened.Close()) }()
	job, err = reopened.Job(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if err := reopened.CheckLedger(ctx, job); err != nil {
		return Job{}, err
	}
	return job, nil
}
