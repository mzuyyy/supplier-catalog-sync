# 09 — Demo runbook and interview preparation

The implementation is available. For verified current commands and results see the root README and `12_VERIFICATION.md`; the exercises below are for the human developer.

## Interview context

The assignment says you will explain your work and make a related change **without generative AI** during the interview; documentation remains allowed. Optimize for a codebase you can navigate, reason about, and modify. Prefer meaningful tests and straightforward function names to impressively large abstractions.

## Five-minute demo plan

**Preparation:** Build binary, start with fresh DB, know fixture expected product versions. Save IDs from `sync list` or start output. Do not reuse a DB where that update ID already completed when demonstrating a failure.

1. **Initial sync:** `sync start --source basic.json`; then `catalog list --include-withdrawn` and `sync changes --job ...`. Point out new, updated and withdrawn rows and durable `COMPLETED` status.
2. **Idempotency:** run same `sync start` again; show same job ID, same product row count and same change ledger length.
3. **Ordering & withdrawal:** show the out-of-order scenario: v3 beats later v1/v2; a withdrawn v5 is not reactivated by later active v3.
4. **Outage:** fresh DB; start with `--fail-before-batch 3`; show `INTERRUPTED`, 2 committed batches, an open outage issue and intact local catalog. Resume without injection. Show final `COMPLETED` and resolved outage issue.
5. **Hard process termination:** fresh DB; use `--delay-between-batches 3s`, kill after two committed batches, restart process and query status; `resume` completes. **Don't call a simulated exception equivalent to a real kill.**
6. **Needs attention:** fresh DB + conflict fixture; show `NEEDS_ATTENTION`, issue code, no partial commit from failing batch, and no false completion.

## Example command sequence (verified during implementation)

```sh
# Build
go build -o catalogsync ./cmd/catalogsync

# Happy path: fresh db
./catalogsync --db ./demo/happy.db sync start --source ./fixtures/updates/basic.json
./catalogsync --db ./demo/happy.db sync list
./catalogsync --db ./demo/happy.db catalog list --include-withdrawn
./catalogsync --db ./demo/happy.db sync changes --job JOB_ID

# Outage: separate fresh db
./catalogsync --db ./demo/failure.db sync start --source ./fixtures/updates/basic.json --fail-before-batch 3
./catalogsync --db ./demo/failure.db sync status --job JOB_ID
./catalogsync --db ./demo/failure.db sync issues --job JOB_ID
./catalogsync --db ./demo/failure.db sync resume --job JOB_ID
./catalogsync --db ./demo/failure.db sync issues --job JOB_ID --all

# Blocking conflict: third fresh db
./catalogsync --db ./demo/conflict.db sync start --source ./fixtures/updates/conflict.json
./catalogsync --db ./demo/conflict.db sync issues --job JOB_ID
```

## Architecture explanation (practice answering in your own words)

**One-minute explanation:**

> “I built a local CLI in Go. A mock supplier provides immutable, versioned events in batches. SQLite holds the product catalog and durable sync jobs. The reducer applies only newer revisions, retains withdrawal tombstones, and treats conflicting same-revision data as an error. Each batch commits its product changes, audit history and checkpoint in one transaction. This makes retries safe and lets a new process resume after a crash. The CLI lets staff see progress, changes and issues.”

### 10 likely technical questions

1. **What if events arrive out of order?** Compare supplier revision; never compare arrival time.
2. **Why use a tombstone?** It remembers a withdrawal and its revision, preventing stale resurrection.
3. **What if the same update is sent twice?** Unique update ID, idempotent same-revision digest, committed batch ledger.
4. **Why per-batch transaction?** Keeps prior completed work and prevents half-batch writes.
5. **What if process is killed during a transaction?** SQLite atomic commit or rollback; checkpoint follows exactly.
6. **What if process is killed immediately after commit?** Reopen DB; committed checkpoint means skip that batch.
7. **How does completion differ from progress?** Completion requires full declared batch coverage, valid final marker, no blocking issues.
8. **What if supplier file is altered before resume?** Reject on source SHA-256 mismatch; checkpoint no longer refers to identical input.
9. **What are the limitations?** Single-process synchronizer, mock source, manual retry, minimal remediation; honest tradeoffs.
10. **How can you inspect the problem?** Status / committed change events / open issues all persisted and queryable.

## Hands-on change rehearsal — without generative AI

Practice **two** extensions entirely on your own after the agent finishes, using Go/SQLite docs if needed.

**Exercise A (15–25 minutes): `catalog show --product P100 --supplier demo-supplier`**

- Add CLI parsing/help.
- Add a repository single-product lookup.
- Print active and withdrawn products including revision/status.
- Add one CLI integration test for found/not found.
- Explain why it is a read-only change and doesn't affect transactions.

**Exercise B (20–35 minutes): `sync changes --type withdrawn` filter**

- Add an optional filter with allowed types `created,updated,withdrawn,reactivated`.
- Implement SQL query filtering (parameterized; no unsafe string concatenation).
- Add tests that unfiltered and filtered queries are consistent.
- Update README usage.

**Optional deeper exercise:** change policy so reactivation is forbidden even with higher revision. Make it a domain reducer rule, add tests, and decide how to report the new blocking condition.

## Source references (for personal study)

- Go database transactions: https://go.dev/doc/database/execute-transactions
- Go `database/sql`: https://pkg.go.dev/database/sql
- SQLite atomic commit overview: https://sqlite.org/atomiccommit.html
- SQLite WAL: https://sqlite.org/wal.html
- Pure-Go SQLite driver: https://pkg.go.dev/modernc.org/sqlite
- Go testing: https://pkg.go.dev/testing

The final submission should link/read these docs only as learning resources, not rely on an unverified claim about the local build.
