# Supplier Catalog Sync

A local Go CLI that imports versioned supplier product events into a durable SQLite catalog. Supports changes, withdrawals, duplicate/out-of-order events, visible issues, and restart/resume after process termination. No server or supplier credentials are required.

## Build and run

Install Go **1.25 or newer** and make sure `go version` works in your terminal. Run all commands from the repository root (the directory containing `go.mod`). The SQLite driver is pure Go, pinned to `modernc.org/sqlite v1.48.2` in `go.mod` and `go.sum`; the normal build needs no C compiler, SQLite installation, database server or supplier credentials.

Windows PowerShell:

```powershell
go mod download
go build -o catalogsync.exe ./cmd/catalogsync
.\catalogsync.exe --help
.\catalogsync.exe --db .\demo\catalog.db sync start --source .\fixtures\updates\basic.json
.\catalogsync.exe --db .\demo\catalog.db sync list
.\catalogsync.exe --db .\demo\catalog.db catalog list --include-withdrawn
```

The build and demos were verified with Go 1.27.3 on Windows/amd64.

Bash (Windows Git Bash uses the `.exe` build shown above):

```bash
go mod download
go build -o catalogsync ./cmd/catalogsync
./catalogsync --db ./demo/catalog.db sync start --source ./fixtures/updates/basic.json
./catalogsync --db ./demo/catalog.db catalog list --include-withdrawn
```

The default DB is `./catalog.db`. Put global `--db` **before** the command. Parent directories and versioned schema migrations are created automatically. Commands below use the Windows binary; on Unix substitute `./catalogsync`.

## Commands

```powershell
.\catalogsync.exe --db .\demo\catalog.db sync start --source .\fixtures\updates\basic.json
.\catalogsync.exe --db .\demo\catalog.db sync list
.\catalogsync.exe --db .\demo\catalog.db sync status --job JOB_ID
.\catalogsync.exe --db .\demo\catalog.db sync changes --job JOB_ID
.\catalogsync.exe --db .\demo\catalog.db sync issues --job JOB_ID
.\catalogsync.exe --db .\demo\catalog.db sync issues --job JOB_ID --all
.\catalogsync.exe --db .\demo\catalog.db sync resume --job JOB_ID
.\catalogsync.exe --db .\demo\catalog.db catalog list
.\catalogsync.exe --db .\demo\catalog.db catalog list --include-withdrawn
```

Replace `JOB_ID` with the ID printed by start or `sync list`. `start` and `resume` run synchronously. Inspection commands read persisted state and return success even when the inspected job needs attention. Start/resume return nonzero for incomplete or blocked updates.

Exit codes: `0` success/read-only inspection, `1` invalid arguments/manifest/unknown job/source identity collision, `2` interruption/source outage/system error, `3` blocking record/source issue. Batch numbers in CLI output and `--fail-before-batch` are **1-based**; persisted indices are **0-based**.

## Five-minute demo

Use a separate fresh DB for each scenario. Repeating a completed update reuses its job; it does not rerun failure injection.

**Happy path and replay:** run the build/start/catalog commands above, then repeat the identical start. It returns the same job with 5 catalog rows, 7 committed changes and unchanged counters: created=4, updated=1, withdrawn=2, duplicate=2, stale=2. Active-only catalog has 3 rows.

| Product | Final revision | Status | Price cents |
|---|---:|---|---:|
| P100 | 3 | active, Mouse Pro | 3200 |
| P101 | 2 | withdrawn, retains Cable metadata | 900 |
| P102 | 5 | withdrawn, unknown-product tombstone | null |
| P103 | 1 | active, Stand | 4500 |
| P104 | 1 | active, Keyboard | 7000 |

**Supplier outage and resume:**

Run the whole block in the same PowerShell window. The first line creates a fresh DB path for each demonstration. Assign `$demoDb` before using it: an unset variable can cause PowerShell to pass `sync` as the value of `--db`.

```powershell
$demoDb = ".\demo\outage-$([DateTime]::UtcNow.Ticks).db"
.\catalogsync.exe --db "$demoDb" sync start --source .\fixtures\updates\basic.json --fail-before-batch 3
# Expected exit 2: INTERRUPTED, 2/3 batches, 4 products, 6 changes.
$demoJob = ((.\catalogsync.exe --db "$demoDb" sync list) -split "\s+")[0]
.\catalogsync.exe --db "$demoDb" sync status --job "$demoJob"
.\catalogsync.exe --db "$demoDb" sync issues --job "$demoJob"
.\catalogsync.exe --db "$demoDb" sync resume --job "$demoJob"
.\catalogsync.exe --db "$demoDb" sync issues --job "$demoJob" --all
```

The last command shows the outage issue resolved after batch 3 commits. `--fail-before-batch` affects only its invocation.

**Conflict:**

```powershell
.\catalogsync.exe --db .\demo\conflict.db sync start --source .\fixtures\updates\conflict.json
# Expected exit 3: NEEDS_ATTENTION, 1/2 batches.
.\catalogsync.exe --db .\demo\conflict.db sync list
.\catalogsync.exe --db .\demo\conflict.db sync issues --job JOB_ID
.\catalogsync.exe --db .\demo\conflict.db catalog list --include-withdrawn
```

Only P100@v1 remains; P200 from the failed batch is rolled back. `invalid.json` similarly commits its first two batches and blocks batch 3. `out_of_order.json` proves v3 wins over v1/v2 and a withdrawn v5 survives late active v3. `reactivate.json` explicitly restores a product at newer revision 6.

**Real force-kill/restart:**

```powershell
go test ./integration -run 'TestHardKillThenNewCLIProcessResumesFromDisk|TestForceKillRollsBackOpenBatchIncludingCompletion' -v -count=1
```

The first test builds the real CLI, waits for committed batch-2 output, force-kills its process and uses new CLI processes for status/catalog/resume. The second force-kills a child with a real open SQL transaction containing product/history/ledger/counter/completion writes. A new connection observes those writes rolled back and resumes successfully. These are actual `Process.Kill` tests, not handled Ctrl+C simulations.

For manual inspection, start with `--delay-between-batches 3s`, force-stop that process after committed progress, then run status/resume from another terminal. Ensure the old synchronizer has stopped before resuming.

## Design

- `internal/supplier`: strict JSON envelopes, immutable file identity, indexed batches and deterministic outages. Event payloads remain raw until their batch, so a bad later event leaves earlier batches committed.
- `internal/domain`: event parsing, canonical SHA-256 and pure `Decide` reducer. Money is integer cents. Newer revisions win; identical same-revision content is a no-op, different content blocks. Unknown withdrawals persist tombstones. Higher active revisions can explicitly reactivate.
- `internal/store`: SQLite migrations and repositories. Tables are `products`, `sync_jobs`, `sync_batches`, `sync_changes`, `sync_issues`.
- `internal/syncer`: lifecycle, transactions, completion and recovery. Each batch atomically commits product effects, old/new snapshots, counters, ledger and checkpoint. A failed batch is rolled back before its issue/state is saved separately. The final transaction checks the complete ledger, end marker and absence of blocking issues before setting `COMPLETED`.
- `internal/cli` and `cmd/catalogsync`: standard-library flags, output, exit codes and signal cancellation.

SQLite uses one pooled connection per Store with per-connection DSN PRAGMAs: `foreign_keys=ON`, `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=FULL`. Migration version is `PRAGMA user_version=1`; migrations never drop catalog data. A commit error triggers a new pool/connection reading the durable job and ledger before deciding whether the batch persisted.

## Assumptions

- **Supplier versions:** every event carries a positive integer `revision` that increases per supplier product. Arrival order and local timestamps do not determine freshness. The supplier must not reuse a revision for different content; such input is a blocking conflict.
- **Identity:** products are identified by `(supplier_id, product_id)`, and updates by `(supplier_id, update_id)`. An update's content is immutable after start.
- **Delta updates:** batches contain product events, not a complete catalog snapshot. An absent product remains unchanged; withdrawal must be explicit.
- **Withdrawal policy:** tombstones retain the latest revision, including withdrawals for unknown products. Older active events cannot restore a withdrawn product. A strictly newer active revision may explicitly reactivate it.
- **Concurrency:** only one synchronizer process runs at a time. Separate read-only inspections are supported, but leases and concurrent-writer coordination are not implemented.
- **Visibility and money:** committed batches are visible even while a job is incomplete. Monetary values are integer `price_cents`, never floating-point amounts.

## Recovery

The source path and SHA-256 of its original bytes are persisted. **Do not edit or replace a fixture after start.** Resume rejects changed content. Missing files may be restored byte-for-byte and retried. Same supplier/update IDs with changed content are a collision. A corrected **new update ID** creates a new job; the original blocked job remains visible.

Counters include only committed inputs, and skipped duplicates/stale records never create change events. A job left `RUNNING` after force termination remains incomplete until explicit resume finishes the remaining work.

## Unfinished work and limitations

No assignment behavior is knowingly left unfinished. The following extensions are **not implemented**:

- A live supplier connector; the demonstration uses fictional JSON fixtures, as permitted by the assignment.
- Automatic retry/backoff or a background scheduler; staff run start/resume manually.
- Multi-writer coordination through leases or fencing; the single-synchronizer assumption applies.
- An operator workflow for repairing/resolving permanent issues; a corrected new update is required, and the original blocked job remains visible.
- Streaming and large-catalog performance work; each fixture is loaded into memory and the implementation targets demonstration-sized data.

A visual interface and public deployment are intentionally omitted because the assignment does not require them. Human interview rehearsal is described in [docs/09_DEMO_AND_INTERVIEW.md](docs/09_DEMO_AND_INTERVIEW.md).

## Tests

From the repository root, run the full suite and static checks:

```powershell
go test ./...
go vet ./...
```

The tests create isolated temporary SQLite files; no existing demo DB, external service or supplier account is required. Coverage includes revision/tombstone rules, duplicate/conflict handling, batch rollback, source identity, persisted progress and actual force-kill/restart recovery. Integration tests build and launch child processes of the CLI.

To run without the test cache and with shuffled test order:

```powershell
go test -shuffle=on -count=1 ./...
```

Optional race detection requires a supported CGO-enabled toolchain and C compiler:

```powershell
go test -race ./...
```

Build, formatting, vet and the full test suite were verified locally. Race detection was attempted but unavailable in the verification environment: `go: -race requires cgo`; enabling CGO then reported that `gcc` was missing. This is an unverified optional check, not a passing race run. The normal application build and tests need no CGO.

See [docs/12_VERIFICATION.md](docs/12_VERIFICATION.md) for observed demo results. [docs/07_IMPLEMENTATION_PLAN.md](docs/07_IMPLEMENTATION_PLAN.md) tracks delivery; the design documents preserve the acceptance contracts. A small interview extension is `catalog show --supplier S --product P`, adding a lookup and found/not-found CLI tests.
