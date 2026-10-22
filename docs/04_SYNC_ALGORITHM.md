# 04 — Synchronization algorithm, invariants and crash recovery

> **Critical spec.** Implement and test this before polishing the CLI.

## Global invariants

1. At most one `products` row per `(supplier_id, product_id)`.
2. For any product, stored `revision` never decreases.
3. If a product is `withdrawn@v5`, no active event with `revision <= 5` can restore it. An explicitly newer active revision is allowed by our assumed supplier semantics.
4. Batch `i` is marked committed **iff** its product effects, history, counters, and `next_batch_index = i+1` all committed in a single transaction.
5. `next_batch_index` only increases, except no movement on a failed batch.
6. Committed changes only: no audit entry for a rolled-back batch.
7. A repeated `(supplier_id, update_id)` returns the same job and does not reapply completed work.
8. A job reaches `COMPLETED` **only if** `next_batch_index == declared_batch_count`, all batch-ledger rows `0..N-1` exist, the trusted last-batch marker was observed and no blocking issues remain open.
9. A crash, supplier outage or invalid record may change job state, but must never erase pre-existing catalog data.
10. Reported counters derive from committed inputs/effects, not failed attempts. `sync_changes` is the source of truth for product mutations.

## Pure reducer decision table

Evaluate an incoming **validated** event against optional existing product:

| Existing | Incoming | Decision | Persisted result |
|---|---|---|---|
| None | active v1 | CREATE | active v1 |
| None | withdrawn v5 | WITHDRAW | tombstone v5 |
| active v2 | active v3 | UPDATE | active v3 |
| active v2 | withdrawn v3 | WITHDRAW | withdrawn v3, keep old display fields |
| withdrawn v5 | active v3 | STALE | withdrawn v5 |
| withdrawn v5 | active v6 | REACTIVATE | active v6 |
| any v5 | any v4 | STALE | existing unchanged |
| any v5 | same event/digest v5 | DUPLICATE | existing unchanged |
| any v5 | different event/digest v5 | CONFLICT | rollback batch, issue |

Do not use supplier arrival timestamps or local update timestamps to decide freshness. Compare integers. Validate an event *before* applying it; reject invalid revision/status/fields with an explicit issue.

## Start algorithm

```text
start(source_path):
  resolve absolute source path
  load manifest + validate supplier ID, update ID, batch count,
    contiguous batch indices and exactly one final marker
  compute SHA-256 of source file
  look up sync_jobs by (supplier_id, update_id)
  if found:
    if hash or source identity changed -> report conflict, preserve job
    else return existing job ID/status without processing any batches
    // if INTERRUPTED or RUNNING, tell operator to use `sync resume --job ...`
  else:
    insert PENDING job (checkpoint 0; counters zero; source path/hash)
    run(job_id)
```

**Important:** manifest validation must not preempt the requirement to **record per-record issues**. The adapter can decode batch envelopes up front, but semantic validation of product events belongs to processing of that batch. An invalid event in batch 3 should leave batches 1–2 committed and produce a queryable `NEEDS_ATTENTION` job. Invalid top-level manifest can be rejected before job creation and shown as CLI error (no persistent job yet).

## Run/resume algorithm

```text
run(job_id):
  load persistent job
  if COMPLETED: return success without mutation
  verify source identity (SHA-256, supplier ID, update ID, declared count)
  if missing/unreadable file: record outage issue and INTERRUPTED
  if changed fixture: record SOURCE_CHANGED blocking issue; NEEDS_ATTENTION
  if status NEEDS_ATTENTION: return actionable error (not success)
  // RUNNING may be reclaimed by resume after hard kill (single-process assumption)
  set RUNNING (durable)

  for i = job.next_batch_index; i < job.declared_batch_count; i++:
    fetch batch i via Source
    if fetch fails: record issue, set INTERRUPTED, return error
    validate exact batch index and terminal marker contract
    BEGIN SQL TRANSACTION
      read checkpoint again; require next_batch_index == i
      verify batch i not already committed
      for each event in batch i in supplied order:
        validate event
        read stored product using same transaction
        decide based on current state (including earlier rows in this batch)
        if conflict/invalid: rollback entire transaction; record blocking issue;
          mark NEEDS_ATTENTION, return error
        CREATE/UPDATE/WITHDRAW/REACTIVATE -> persist product + sync_changes
        STALE/DUPLICATE -> no product/change write; count skip
      insert sync_batches(job_id, i, batch_digest, ...)
      set next_batch_index=i+1, aggregate counters
      resolve previous transient outages for batch i
      if i is last declared batch:
        verify `is_last == true`, no open blocking issues,
          all checkpoints and ledger entries present (incl current batch)
        set status=COMPLETED, finished_at=now
      else leave status=RUNNING
    COMMIT
    if commit error: keep job non-complete; reopen DB and inspect persisted state;
      retry safely from durable checkpoint (don't blindly assume failure/success)

  return final durable status from database
```

**Commit ambiguity:** if the client sees an error during `Commit`, the application cannot simply assume rollback. On next run, inspect `sync_batches` and `next_batch_index` in the database before deciding which batch to retry. Idempotent reducer/unique constraints protect against replay. Do not perform a second product write based purely on an in-memory counter.

## Batch atomicity example

Before: `checkpoint=2`, batch 2 contains `P10@v4` and then an invalid record.

Incorrect: P10@v4 persists, checkpoint still 2, no way to know whether replay is safe.

Correct: batch transaction rolls back **including P10 update and audit row**; checkpoint remains 2; previously committed batches 0 and 1 remain; `sync_issues` gets one durable blocking entry **outside** that rolled-back transaction.

## Hard kill and resume example

1. Import batches 0 and 1: each commits a batch row and new checkpoint.
2. Kill process with `SIGKILL` during batch 2; process cannot clean up `RUNNING` state.
3. New process opens the same SQLite path. `sync status` shows `RUNNING` with `next_batch_index=2`, **not** `COMPLETED`.
4. `sync resume --job ...` re-verifies source hash and replays starting with batch 2.
5. Any pre-kill commit is either fully present with its checkpoint or fully absent; no half-committed batch.
6. Final batch commits and job is marked complete atomically.

## Failure classification

| Condition | Behavior | Job state |
|---|---|---|
| File temporarily unavailable / injected supplier outage | Preserve earlier commits; append/reuse open transient issue | `INTERRUPTED` |
| Application killed | Last durable state may be `RUNNING`; resume reclaims after restart | `RUNNING` until resumed |
| Context canceled between batches | Persist interruption where possible | `INTERRUPTED` |
| Invalid product event | Rollback its entire batch, open blocking issue | `NEEDS_ATTENTION` |
| Same revision with different content | Rollback its entire batch, open blocking issue | `NEEDS_ATTENTION` |
| Source digest changed on resume | Don't use changed file/checkpoint; create issue | `NEEDS_ATTENTION` |
| Disk/SQL failure | Rollback when possible, report error; never claim completed | non-complete / error |
| Duplicate or stale event | Ignore product change; count within successful batch | Continue normally |

## Completeness and empty updates

The fixture contract requires at least one batch to avoid an ambiguous “zero batches == complete” shortcut. An empty batch is valid if its index and final marker are valid. Only a complete manifest with all declared batches and an authenticated local end marker satisfies the completion gate; no missing batch can be interpreted as a withdrawal.

## Operator-visible output

- `sync status`: state, supplier/update IDs, committed/total batches, created/updated/withdrawn/reactivated/stale/duplicate counters, outstanding blocking issue count.
- `sync changes`: committed old/new snapshots by product and batch.
- `sync issues`: open issues first, with batch, product, code, diagnostic and whether retry may help.

## Tests that cannot be skipped

1. Withdrawal v5 + late active v3 keeps withdrawal.
2. Same revision equal digest is idempotent; same revision different digest blocks.
3. Unknown product withdrawal produces tombstone.
4. Crash/restart after committed batches preserves them and finishes remaining work.
5. Failed batch writes no products, change events, counters or checkpoint advancement.
6. Simulated outage doesn't erase existing catalog.
7. Missing final marker cannot result in `COMPLETED`.
8. Re-running completed update doesn't add history or change counts.
