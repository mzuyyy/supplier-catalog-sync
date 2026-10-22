# 05 — Staff CLI behavior and command examples

## CLI name

`catalogsync`. Use `flag` / standard library subcommand parsing; keep dependencies light. Global `--db` before command; default `./catalog.db`. Output readable plain text, deterministic ordering. Optional `--json` is nice-to-have, not necessary.

## Commands

### Start an update

```sh
catalogsync --db ./demo/catalog.db sync start --source ./fixtures/updates/basic.json
```

- Creates the database parent directory if missing; opens DB and applies migrations if needed.
- Validates source update identity and creates a persistent job.
- Outputs stable `job_id`, status and batch progress.
- Runs synchronously until completion or interruption.
- Repeating with same supplier/update/hash must reuse job and **not** reapply batches. If already complete, return success and print that the update was already imported. If interrupted, print instructions to use `sync resume`.
- A different file with the same supplier/update ID is a source collision, not a second job.
- Return nonzero on errors, `INTERRUPTED` or `NEEDS_ATTENTION`.

### Simulated supplier failure (for proof)

```sh
catalogsync --db ./demo/catalog.db sync start \
  --source ./fixtures/updates/basic.json --fail-before-batch 3
```

- Flag is **1-based**: inject a deterministic transient fetch error before third batch, after batches 1 and 2 have committed.
- Must show a non-success job state and preserve the catalog.
- Issue must include simulated outage and batch number.
- This flag affects **this invocation only**; it is not persisted. Resume without flag should proceed.

### Deterministic delay for manual hard-kill demonstration

```sh
catalogsync --db ./demo/catalog.db sync start \
  --source ./fixtures/updates/basic.json --delay-between-batches 3s
```

- Optional test/demo flag. Sleep **after a committed nonfinal batch**, before fetching the next.
- During delay, manually kill the process, start a new process, inspect job and resume.
- Do not sleep inside a database transaction. In tests prefer a deterministic hook/barrier over time-sensitive sleep.

### Resume

```sh
catalogsync --db ./demo/catalog.db sync resume --job JOB_ID
```

- Reopens persisted job and immutable source location/hash.
- Continues at `next_batch_index`; never repeats already committed progress as a new change.
- A job still marked `RUNNING` after hard kill can be resumed under single-synchronizer assumption.
- A `NEEDS_ATTENTION` job cannot silently become successful without resolving the underlying issue; show instructions to inspect issues.
- On a completed job, return success/no-op.

### Status

```sh
catalogsync --db ./demo/catalog.db sync status --job JOB_ID
catalogsync --db ./demo/catalog.db sync list
```

`sync list` is recommended for finding job IDs after restart. Each row includes ID, supplier/update ID, state, progress and last update time. `sync status` includes counters and open-issue count. Status must always be queried from DB. Use stable state names (`PENDING`, `RUNNING`, `INTERRUPTED`, `NEEDS_ATTENTION`, `COMPLETED`).

Example (illustrative only):

```text
Job: JOB-001
Supplier: demo-supplier
Update: update-2026-001
Status: INTERRUPTED
Batches committed: 2/3
Changes: created=2 updated=0 withdrawn=1 reactivated=0
Skipped: duplicate=0 stale=1
Open blocking issues: 0 | Other open issues: 1
Next action: catalogsync ... sync resume --job JOB-001
```

### Product catalog

```sh
catalogsync --db ./demo/catalog.db catalog list
catalogsync --db ./demo/catalog.db catalog list --include-withdrawn
```

Default shows active products; `--include-withdrawn` also displays tombstones. Show supplier, product ID, revision, status, name, price cents. A withdrawn product with unknown metadata may have empty name and price.

### Change history

```sh
catalogsync --db ./demo/catalog.db sync changes --job JOB_ID
```

List committed product changes with type, product, old and new values, batch, revision. Deterministic order (batch then change ID). Repeated imports must not duplicate entries.

### Issues needing attention

```sh
catalogsync --db ./demo/catalog.db sync issues --job JOB_ID
catalogsync --db ./demo/catalog.db sync issues --job JOB_ID --all
```

Default shows open issues, with `blocking` vs transient and clear messages. `--all` includes resolved past outages for auditing. Recoverable outage issue is automatically marked resolved only after the affected batch commits successfully.

## Exit codes

- `0`: requested command completed successfully (including read-only inspection and completed no-op).
- `1`: user error, invalid command/arguments or source input that prevents start.
- `2`: update interrupted, cannot reach supplier source, or unexpected system error.
- `3`: update needs manual attention due to a blocking issue.

Exact integer codes are less important than consistency, nonzero failure and documentation, but implement these unless technical reasons demand otherwise.

## Ergonomics

- Print a job ID as soon as possible after job creation, including failure path.
- Use `stdout` for normal output, `stderr` for errors; don't print “sync completed” on error.
- Explicitly warn that mock fixture mutation after start invalidates safe resume.
- Ensure examples work in Bash and show Windows PowerShell equivalent in final README if practical.
- Never require a running server, browser, live supplier credentials or cloud resources.
