# AGENTS.md — Coding Agent Instructions

> This repository starts as an **implementation blueprint**, not a completed application. Your job is to implement, test, and document a runnable Go program. Do not claim any command works until you have run it.

## Mission

Implement a local supplier-catalog synchronization product in Go. Staff must be able to start an update, inspect its outcome, see committed changes and unresolved problems, and resume after application restart. Suppliers deliver versioned product events in batches, including withdrawal events, duplicates and out-of-order records. The existing catalog must survive supplier outages and process termination.

## Required reading order

1. `docs/01_REQUIREMENTS.md` — acceptance criteria and boundaries.
2. `docs/02_ARCHITECTURE.md` — boundaries, interfaces and proposed layout.
3. `docs/03_DATA_CONTRACT.md` — fixture and persistence contracts.
4. `docs/04_SYNC_ALGORITHM.md` — transaction and recovery rules (highest-priority technical contract).
5. `docs/05_CLI_SPEC.md` — exact command behavior.
6. `docs/06_TEST_PLAN.md` — executable tests and crash simulation.
7. `docs/07_IMPLEMENTATION_PLAN.md` — ordered tasks and definition of done.
8. `docs/08_ASSUMPTIONS_AND_TRADEOFFS.md` — deliberate scope decisions.
9. `docs/09_DEMO_AND_INTERVIEW.md` — final demonstration and explainability.

If documents disagree: original assignment requirements > correctness invariants in `04_SYNC_ALGORITHM.md` > data contract > CLI contract > implementation plan. Resolve differences by updating all affected docs, not by quietly inventing behavior.

## Non-negotiables

- **Go**, not another language. Implement a small CLI; no web UI or deployment is necessary.
- Use `database/sql` with SQLite, real disk persistence, and database transactions.
- Each supplier product is identified by `(supplier_id, product_id)`; don't create duplicates.
- Compare **supplier revision**, not arrival order, to decide whether incoming data is newer.
- Keep withdrawn-product tombstones; old active records must never restore them.
- The **product writes, change events, progress counters, batch ledger and checkpoint** for one batch must commit in **one SQL transaction**.
- Never delete/rebuild the existing catalog before an update.
- Don't report `COMPLETED` until every declared batch is successfully committed and a valid end marker is seen.
- A failed, blocked, or interrupted run must remain distinguishable from completed.
- After force termination, a new process reading the same SQLite file must be able to resume without duplicating changes.
- An identical product revision with different content is a **conflict**, not a silent overwrite.
- Staff must be able to list open issues and committed changes for a job.
- Always check and propagate errors; no swallowed database/IO failures.

## Chosen stack / boundaries

- Go >= 1.25 and `modernc.org/sqlite` at **v1.48.2** (pure Go driver; pin in `go.mod`, then `go mod tidy`). If local tooling cannot run this version, choose a compatible pinned version and explicitly document the deviation.
- Go standard library for CLI flag parsing, JSON, hashing, logging and tests; avoid unnecessary frameworks.
- Synchronous command (`sync start` / `sync resume`) is acceptable. Progress lives on disk, not in memory alone.
- Read from fictional JSON supplier fixtures; support deterministic simulated outages and forced process interruption.
- No distributed workers, multiple active writers per supplier or live supplier account are required. Explicitly document the **single synchronizer process** assumption.

## Implementation style

- Separate CLI parsing, domain sync rules, source adapter and storage. Prefer explicit structs/interfaces over magical frameworks.
- Introduce repository methods that accept `*sql.Tx` for writes in a batch; never mix `*sql.DB` updates into an open batch transaction.
- Treat monetary values as integer `price_cents`; do not use floats.
- Use contexts on DB operations; signal cancellation should terminate cleanly at a safe boundary when possible, but **SIGKILL recovery** must not rely on handlers.
- Keep migrations versioned and repeatable, apply at startup, and never drop catalog tables during normal runs.
- Write table-driven unit tests and real SQLite integration tests; inspect temporary disk DBs using a **new connection** after a simulated restart.
- Optimize for code you can confidently explain and modify without AI in an interview.

## Required workflow

1. Inspect repository and read all docs before writing code.
2. Translate `docs/07_IMPLEMENTATION_PLAN.md` into your working task list; keep progress updated there.
3. Implement source loader + validation, schema, reducer, transactional batch import, job manager, CLI, fixtures, tests, docs, in that order.
4. Run `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...` where supported, and an actual CLI demonstration with a persistent DB.
5. Demonstrate a real process restart (new process/new DB connection); do not substitute an in-memory retry test.
6. Update `README.md` with verified install/run/demo/test commands, design rationale, assumptions and **honest unfinished work**.
7. Finish with a short implementation report: files changed, commands run, outcomes, known limitations, and one proposed interview-sized extension.

## Don't do these things

- Do not build a React app, Kubernetes deployment or message queue before core invariants/tests pass.
- Do not treat a duplicate update as a new change or advance counters twice.
- Do not equate “no next page available” with “success” unless the declared source manifest/end marker was verified.
- Do not recover from an invalid record by silently discarding it and marking the job complete.
- Do not let an `Interrupted` or `NeedsAttention` job be printed as a success.
- Do not claim exactly-once delivery from the supplier. Implement effectively-once **database effects** through idempotency and transactional checkpoints.
