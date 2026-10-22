# 07 — Implementation plan / working checklist

Implementation and automated verification completed on 2026-10-22; see `12_VERIFICATION.md`. Human interview rehearsal remains open.

This file is the agent's task tracker. Update `[ ]` to `[x]` only when code and tests exist. Aim for the smallest **complete**, explainable product, not the biggest architecture.

## Phase 0 — Set up a compilable repository

- [x] Initialize Go module, pin Go/toolchain and `modernc.org/sqlite` compatible version.
- [x] Create `cmd/catalogsync` entrypoint and `internal` packages from architecture spec.
- [x] Establish a clear error model / CLI exit codes; make help output work.
- [x] Ensure `go test ./...` runs even before business logic is finished.

**Gate:** Go module and binary compile.

## Phase 1 — Source fixture contract

- [x] Implement `Manifest`, `Batch`, `ProductEvent` typed JSON structures.
- [x] Validate supplier/update IDs, declared count, batch index continuity, final marker.
- [x] Implement canonical event SHA-256 digest, stable across runs.
- [x] Implement source file path + SHA-256 identity checks.
- [x] Add fixture files `basic`, `out_of_order`, `conflict`, `invalid`, `reactivate`.
- [x] Implement fetch failure injection before a 1-based batch number.
- [x] Tests for malformed manifest and semantic record validation timing.

**Gate:** Mock source is deterministic; invalid *batch events* remain reportable as persisted issues during sync.

## Phase 2 — SQL persistence

- [x] Implement versioned idempotent schema migrations.
- [x] Create tables/constraints/indexes for products, jobs, batch ledger, changes and issues.
- [x] Enable/verify foreign keys, busy timeout, disk persistence.
- [x] Implement repository reads and writes with transaction-aware APIs.
- [x] Add DB reopen persistence test.

**Gate:** Tables survive restart; uniqueness and checks reject corrupt writes.

## Phase 3 — Pure domain logic

- [x] Validate events: revision, ID, status, required fields, price cents.
- [x] Implement `Decide(existing,event)` decision table.
- [x] Implement `withdrawn` tombstones including unknown products.
- [x] Handle same-revision duplicate vs conflict using digest.
- [x] Explicitly support higher-revision reactivation.
- [x] Table-driven unit tests for all decision branches.

**Gate:** Unit tests cover stale, same-revision and withdrawal behavior.

## Phase 4 — Transactional batch sync

- [x] Implement start job, immutable source identity and already-imported update detection (repeated start never re-executes an existing job; resume is explicit).
- [x] Implement per-batch SQL transaction containing: product changes, audit, counters, batch ledger, checkpoint.
- [x] Rollback entire batch for invalid or conflicting event; persist an issue after rollback.
- [x] Persist outage issue and interrupted status outside failed batch transaction.
- [x] Implement completion gate atomically with final batch commit.
- [x] Make retries use persisted checkpoint, not in-memory index.
- [x] Add tests for partial batch rollback, replay and idempotency.

**Gate:** AC-01 through AC-10, AC-12 pass.

## Phase 5 — Restart / resume behavior

- [x] Implement `resume(job_id)` from SQLite file, including a stale `RUNNING` state caused by SIGKILL under documented single-process assumption.
- [x] Re-verify source digest before resume.
- [x] Resolve transient outage issue only once corresponding batch commits.
- [x] Make `NEEDS_ATTENTION` impossible to silently declare complete.
- [x] Add reopen-with-new-DB-connection test.
- [x] Add true separate-process hard-kill test or a documented equivalent manual demo.

**Gate:** Job interrupted after 2 committed batches can complete in a separate process without duplicate products or history.

## Phase 6 — Operator CLI

- [x] `sync start --source ...` (with `--fail-before-batch` and optional delay).
- [x] `sync resume --job ...`.
- [x] `sync status --job ...` and `sync list`.
- [x] `sync changes --job ...`.
- [x] `sync issues --job ... [--all]`.
- [x] `catalog list [--include-withdrawn]`.
- [x] Human-readable diagnostics and proper nonzero exit codes.
- [x] CLI integration tests of non-success status and state after restart.

**Gate:** All staff user journeys demonstrable from a terminal.

## Phase 7 — Delivery / reviewer preparation

- [x] `gofmt` on source files and tests.
- [x] `go vet ./...` and `go test ./...` pass; attempt `go test -race ./...`.
- [x] Verify real run instructions on clean DB (no untracked local assumptions).
- [x] Verify `README.md` no longer says “blueprint”; show exact working commands and expected results.
- [x] Document assumptions and known missing features honestly.
- [x] Record proof: status before interruption, after restart, final completion; catalog before/after; changelog and issues.
- [ ] Human exercise: rehearse one small related change without generative AI (see interview guide); the agent cannot perform or certify this.

**Gate:** Reviewer can clone and run everything locally with Go, and developer can explain every important invariant.

## Prioritization under pressure

**P0:** schema, update reducer, transactions/checkpoint, resume, status, tests, README.

**P1:** audit change diffs, issues, failure injection, clear CLI output, true process-kill demo.

**P2:** pretty tables, JSON mode, exponential backoff, cancellation ergonomics, progress polish.

Do **not** cut P0 reliability tests just to add P2 functionality.
