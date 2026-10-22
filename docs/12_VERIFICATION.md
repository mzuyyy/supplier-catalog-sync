# Verification record

Verified on 2026-10-22, Windows/amd64, Go 1.27.3, modernc.org/sqlite v1.48.2. The module minimum remains Go 1.25.0. The Go archive was downloaded from go.dev and its SHA-256 checked against official release metadata. The toolchain is installed under the user's local share directory; no global PATH or administrator setup was changed.

## Commands executed

- `go mod tidy`: passed; go.mod/go.sum pin the requested driver and transitive dependencies.
- `gofmt -w cmd internal integration`: applied.
- `go vet ./...`: passed.
- `go test ./...`: passed.
- `go test ./... -shuffle=on -count=1 -timeout=120s`: passed, including both actual process-kill tests.
- `go build -o catalogsync.exe ./cmd/catalogsync`: passed.
- `go test -race ./...`: attempted, unavailable: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`. No C compiler was available on PATH. No passing race claim is made.
- LSP diagnostics: attempted through MCP; transport closed. Build and vet provide the available compiler/static checks.

## Observed CLI results on persistent DB files

Commands were run as independent CLI processes, using a separate fresh DB for happy path, outage and conflict. Local artifacts are ignored under `demo/`.

| Scenario | Observed result |
|---|---|
| basic.json | exit 0, COMPLETED, 3/3 batches, 5 rows, 7 committed changes |
| repeat same start | exit 0, same job ID, same counters/history |
| active catalog | P100@v3, P103@v1, P104@v1 |
| withdrawn catalog | P101@v2 retains Cable/900; P102@v5 has null metadata |
| fail before batch 3 | exit 2, INTERRUPTED, 2/3, 4 rows, 6 changes, one open outage |
| new process resume | exit 0, COMPLETED, 3/3, 5 rows, 7 changes, outage resolved |
| conflict.json | exit 3, NEEDS_ATTENTION, 1/2, one blocking REVISION_CONFLICT |
| conflict catalog | only P100@v1; P200 from failed batch absent |

The final basic counters were created=4, updated=1, withdrawn=2, reactivated=0, duplicate=2, stale=2.

## Reliability evidence

- `TestHardKillThenNewCLIProcessResumesFromDisk`: builds real CLI, observes batch 2 committed, force-kills it, uses new processes for status/catalog/resume, verifies five unique products and seven changes. A second completed resume is a no-op.
- `TestForceKillRollsBackOpenBatchIncludingCompletion`: force-kills a child after transaction-local product/history/ledger/counter/checkpoint/COMPLETED writes, before commit. New connection sees RUNNING at checkpoint 2, no uncommitted product/history, then resumes.
- `TestSQLFailureRollsBackEveryBatchEffect`: SQL triggers inject failures in product, history, ledger, checkpoint/counters and deferred-FK commit; earlier batch survives, failing batch has no effects, and resume works after removing the test fault.
- Additional tests cover immutable source hashes, missing/restored source, malformed manifest, invalid later event, duplicate issue prevention, repeated starts, different update replay, reactivation, ledger corruption and open blocking completion gate.
- SQLite foreign keys, WAL, busy timeout and FULL synchronous mode were checked on replacement connections. Valid paths with spaces, `#`, and Vietnamese characters were exercised. A test initially included `?`, an invalid Windows filename; direct filesystem checks confirmed the test input was invalid, and it was corrected without changing production path handling.

## Delivery boundaries

Single synchronizer process, mock JSON transport and manual retry are intentional scope limits. The supplied workspace initially had no Git repository. After implementation verification, Git was initialized on `main` for publication at the user's request; no PR is part of this initial publication. Human interview rehearsal/checklist items cannot be claimed by the coding agent; they remain explicitly open. See README for usage and limitations.

## Final suite and independent review

The final uncached, shuffled suite passed **84 tests/subtests**, zero failures, across domain, supplier, store, CLI and integration packages. `gofmt -l` reported no files, `go vet ./...` passed, and `go mod verify` reported `all modules verified`. README build/start/catalog commands were also exercised in native PowerShell.

Five independent review lanes passed: goal/constraint verification, hands-on CLI QA, code quality, security and context/spec consistency. QA exercised 53 CLI scenarios, all passing; its local transcript is under ignored `demo/review-qa-20261022-a17/`.

The security review found a low-severity terminal-control injection in externally supplied IDs. `safeText` now escapes control characters in supplier/update/product display fields and last-error output. A regression test was observed failing before the fix and passing afterward across list/status/issues/changes/catalog. A failing-output-writer test also verifies non-success and a durable resumable job when stdout breaks.

Remaining verification improvement: the deferred-FK fault test proves commit-error rollback/reconciliation. A synthetic acknowledgement error after a successful durable commit is not directly injected; the implemented recovery branch rereads job/checkpoint/ledger on a new connection, and real post-commit force-kill tests verify durable replay behavior.

The final security follow-up passed after escaping both stderr diagnostics and persisted LastError. Each path has a failing-first ESC/newline regression test. Running with CGO_ENABLED=1 also confirmed the race prerequisite is missing: `cgo: C compiler "gcc" not found`; this is an environment limitation, not a passing race run.
