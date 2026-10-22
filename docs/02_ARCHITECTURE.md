# 02 — Architecture and module boundaries

## Decision: Go CLI + embedded SQLite + JSON mock supplier

Simplest architecture that proves the assignment's reliability requirements:

```text
Staff terminal
  └── catalogsync CLI (cmd/catalogsync)
      ├── command handlers: start, resume, status, changes, issues, catalog
      ├── sync application service
      │   ├── fixture source adapter (JSON manifest, batches, injected failures)
      │   ├── pure domain reducer (revision rules, validation, change classification)
      │   └── transactional repository (SQLite)
      └── SQLite disk file
          ├── products + tombstones
          ├── sync_jobs + sync_batches
          ├── sync_changes
          └── sync_issues
```

CLI start/resume may execute synchronously. Staff can run follow-up commands after they return, or read persisted state from another process while a run is in progress if SQLite access permits. The job is not ephemeral: **status is read from SQLite**, never just an in-memory structure.

## Suggested Go layout

```text
.
├── AGENTS.md
├── README.md
├── go.mod
├── go.sum
├── cmd/
│   └── catalogsync/
│       └── main.go
├── internal/
│   ├── cli/
│   │   ├── cli.go
│   │   └── cli_test.go
│   ├── domain/
│   │   ├── product.go
│   │   ├── apply.go
│   │   └── apply_test.go
│   ├── supplier/
│   │   ├── source.go
│   │   ├── fixture.go
│   │   └── fixture_test.go
│   ├── syncer/
│   │   ├── service.go
│   │   └── service_test.go
│   └── store/
│       ├── sqlite.go
│       ├── migrations.go
│       ├── products.go
│       ├── jobs.go
│       └── sqlite_test.go
├── fixtures/
│   └── updates/
│       ├── basic.json
│       ├── out_of_order.json
│       └── conflict.json
├── integration/
│   ├── sync_test.go
│   └── restart_test.go
└── docs/
    └── ...
```

Adapt if a smaller structure is clearer, but preserve testable boundaries.

## Proposed key contracts

```go
// Supplier event; validated before application.
type ProductEvent struct {
    ProductID string
    Revision int64
    Status ProductStatus // active | withdrawn
    Name *string         // required only for active
    PriceCents *int64    // required only for active
}

// Adapter loads a stable update manifest and a specific batch.
// An injected outage happens BEFORE the requested batch is returned.
type Source interface {
    Manifest(ctx context.Context) (Manifest, error)
    Batch(ctx context.Context, zeroBasedIndex int) (Batch, error)
}

// Pure: decide create/update/withdraw/reactivate/ignore/conflict
// without I/O. ExistingProduct may be nil.
func Decide(existing *Product, event ProductEvent) (Decision, error)
```

Don't force these exact signatures if better alternatives emerge. Keep pure business decisions separate from SQL and CLI output.

## Responsibility boundaries

- **`supplier`**: reads file, validates schema/manifest, serves batches by deterministic index. No database mutations.
- **`domain`**: validates event semantics, canonical fingerprint, compares revisions, classifies decisions. No file or DB access.
- **`store`**: schema, SQL constraints, transactional read/write, batch ledger, consistent timestamps.
- **`syncer`**: coordinates lifecycle, sources, transactions, retry/resume, completion gate, issue recording.
- **`cli`**: parses arguments, chooses exit codes, produces human-readable status and lists. Business logic must not live here.

## Persistence and lifecycle

1. Validate source manifest before creating a job.
2. Insert `PENDING` job with immutable source identity (absolute path, SHA-256 content digest, supplier/update IDs, declared batch count).
3. Move to `RUNNING` before processing.
4. For batch `i = next_batch_index`, load batch, validate and atomically apply all of its records with the batch checkpoint in one SQL transaction.
5. On recoverable fetch/outage error, record issue, mark `INTERRUPTED`, return nonzero; preserve catalog and checkpoint.
6. On malformed event or revision conflict, rollback current batch; record blocking issue, mark `NEEDS_ATTENTION`, return nonzero.
7. After final batch, mark `COMPLETED` **in that batch's transaction** only if the trusted manifest/end marker and zero-blocking-issues conditions hold.
8. On restart, read job/checkpoint from disk. A job left `RUNNING` after an abrupt kill may be reclaimed by `resume` under the documented single-process assumption.

## SQLite configuration

- Set `PRAGMA foreign_keys = ON` on every connection or use a DSN that does so; test actual behavior.
- Enable WAL and `busy_timeout` where supported, and document precise DSN/PRAGMA setup.
- Use `database/sql` transactions (`BeginTx`, `Commit`, `Rollback`).
- To make per-connection PRAGMAs reliable, prefer `db.SetMaxOpenConns(1)` for this local single-process MVP, or establish connection init logic for every connection. If enforcing one connection, acknowledge that concurrent status reads in the same process may wait.
- Use `context.Context` consistently. A canceled context must not produce a fake committed checkpoint.

## Constraints and risks

- A JSON file is a stand-in for a supplier endpoint; injected failures simulate supplier outages. It isn't a live network integration.
- Source immutability check protects correctness on resume, but won't permit replacing a fixture in place to fix bad data. A new corrected update ID is the documented way forward in MVP.
- Single synchronizer process assumption avoids designing leases/fencing unnecessarily; multi-process concurrent writers are explicitly future work.
- `SIGKILL` won't execute cleanup hooks; a `RUNNING` job after a crash is **not** evidence of completion.
