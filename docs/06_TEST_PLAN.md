# 06 — Verification plan: unit, integration and fault injection

## Testing approach

Use Go `testing` + table-driven tests, no external test service. Use `t.TempDir()` for file-backed SQLite DBs and independent fixture copies. Most important tests must exercise real SQL transactions; pure reducer tests alone are not sufficient.

Run at minimum:

```sh
gofmt -w ./cmd ./internal ./integration
go vet ./...
go test ./...
go test -race ./...
go build -o catalogsync ./cmd/catalogsync
```

The `-race` test may be platform/toolchain dependent; record genuine results, do not claim it ran when it didn't.

## Unit tests: `domain`

| Test ID | Setup | Expectation |
|---|---|---|
| U01 | nil product + active v1 | create |
| U02 | nil product + withdrawn v5 | tombstone |
| U03 | active v1 + active v2 | update |
| U04 | active v3 + active v1 | stale, no change |
| U05 | withdrawn v5 + active v3 | stale, remains withdrawn |
| U06 | withdrawn v5 + active v6 | reactivated (explicit assumption) |
| U07 | same revision, same normalized payload | duplicate |
| U08 | same revision, different name/price/status | conflict |
| U09 | same product with zero/negative revision | invalid |
| U10 | active with missing name/negative price | invalid |
| U11 | input record has different field order, but same semantic content | same canonical digest |

## Fixture/contract tests: `supplier`

| Test ID | Setup | Expectation |
|---|---|---|
| S01 | manifest count matches batches, last marker correct | accepted |
| S02 | missing/duplicate batch index | rejected before job |
| S03 | no final marker / early multiple final markers | rejected, never completed |
| S04 | same update ID with different file hash | collision error, no mutation |
| S05 | event semantically invalid in later batch | earlier batches can commit; current blocked with issue |
| S06 | `--fail-before-batch 3` | fetch fails deterministically before batch #3 |

## SQLite/service integration tests

| Test ID | Scenario | Assert |
|---|---|---|
| I01 | Import complete 3-batch fixture | catalog matches expected; status complete; checkpoint=3 |
| I02 | Run exact same update again | same job; same row counts, counters and change event IDs |
| I03 | Import different update containing already-known revisions | no duplicate products; no stale overwrites |
| I04 | Out of order v3,v1,v2 | version 3 wins; stale counters correct |
| I05 | withdrawn@v5 then active@v3 | withdrawn@v5 remains; no restoration |
| I06 | withdrawn unknown product | tombstone saved; active-only view hides it |
| I07 | same revision conflicting payload in batch 2 | batch 2 rolled back; blocking issue open; job needs attention |
| I08 | invalid record after a valid event in same batch | valid event from failed batch also rolled back |
| I09 | outage before batch 3 | 2 batches committed; job interrupted; old catalog preserved |
| I10 | resume I09 in brand new service with new DB connection | processes remaining batch; job complete; no duplicate effects; transient issue resolved |
| I11 | file changes between outage and resume | job not complete, SOURCE_CHANGED issue, prior data intact |
| I12 | source lacks completion marker | start rejected; zero completed jobs |
| I13 | upsert/change-history/checkpoint/counters fail mid-transaction (test hook or SQL constraint) | whole batch rollback |
| I14 | restart after last batch committed | no repeated change events; status completed |
| I15 | all previous catalog entries survive a bad new update | old catalog unchanged except prior committed valid batches |

## True process restart test (critical)

For a strong demonstration, use a **subprocess** test or a clearly repeatable manual sequence:

1. Create a fresh temporary SQLite file.
2. Spawn compiled CLI with a 3+ batch fixture and `--delay-between-batches` long enough to observe 1–2 committed batches.
3. Verify checkpoint from a separate SQLite connection or CLI `sync status` (poll until `next_batch_index >= 2`).
4. Kill the subprocess forcefully (`Process.Kill`, not just a handled interrupt).
5. Start a completely new CLI process against **the same DB file**.
6. Verify job is not completed; old catalog is queryable; next checkpoint is at the correct boundary.
7. Run `sync resume --job ...` in new process.
8. Assert completed status, exactly one product row per identity and exactly one change event per applied revision.

If subprocess tests prove flaky on some OS, retain a deterministic service-level reopen/restart test, plus manual hard-kill demo instructions. Do **not** present graceful `Ctrl+C` alone as proof of SIGKILL recovery.

## Assertions to implement as helpers

- `countProducts(supplier, productID) == 1`.
- `getRevision(productID) == max(valid applied revisions)`.
- `getProduct(...).Status == withdrawn` after stale late active.
- `getJob(...).NextBatchIndex` never advances on failed batch.
- `countChanges` equals count of genuine committed mutations, unchanged after replay.
- `countOpenBlockingIssues` > 0 implies `status != COMPLETED`.
- Every committed batch index has one ledger row and a matching contiguous checkpoint.

## CLI smoke acceptance script (manual or automated)

```sh
# Replace executable path for Windows and run from repo root.
./catalogsync --db ./demo/test.db sync start --source ./fixtures/updates/basic.json
./catalogsync --db ./demo/test.db sync list
./catalogsync --db ./demo/test.db catalog list --include-withdrawn
./catalogsync --db ./demo/test.db sync changes --job JOB_ID
./catalogsync --db ./demo/test.db sync issues --job JOB_ID --all
# Repeat start: same job, no extra changes.
./catalogsync --db ./demo/test.db sync start --source ./fixtures/updates/basic.json
```

Use a **different fresh DB file** for the failure demo so `update_id` is not already completed. Report actual tested command outcomes in README.
