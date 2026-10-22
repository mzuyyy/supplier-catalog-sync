# 08 — Assumptions, tradeoffs, and unfinished work

These are **deliberate decisions**; explain them in the final README. The original assignment is brief and does not mandate a protocol or framework.

## Decisions and rationale

| Area | Decision | Reason / tradeoff |
|---|---|---|
| Operator experience | CLI rather than UI | Assignment permits it; focus on reliability, testability and simplicity |
| Backend | Single Go binary | Easy to review and run; no server dependency |
| Database | SQLite file through `database/sql` | Transactional, durable, zero infrastructure |
| Driver | Pure-Go `modernc.org/sqlite` pinned to version compatible with Go toolchain | Avoid CGO setup; lock dependencies in go.mod/go.sum |
| Supplier transport | Immutable JSON fixture with multiple batches | Meets fictional-supplier allowance; reproducible failures |
| Freshness | Positive, per-product monotonic integer `revision` supplied by supplier | Arrival time is unsafe for out-of-order events |
| Update identity | `(supplier_id, update_id)` unique and immutable | A repeated update must not produce duplicate effects |
| Completion evidence | Count, contiguous indices and explicit final marker | A short source read/outage isn't proof of completion |
| Withdrawal | Soft delete / tombstone, retain last known display fields | Prevent late stale events from restoring withdrawn products |
| Reactivation | Allow **strictly newer** active revision after withdrawal | Reasonable supplier semantics; stale reactivation forbidden |
| Atomicity | Batch-level transaction, not entire update transaction | Preserve completed work and allow bounded retries |
| Partial update visibility | Already committed batches visible in local catalog | Local copy stays available; job status says incomplete |
| Issue handling | Expose open/auto-resolved transient issues; block on permanent invalid/conflicting data | Required visibility without building full operator remediation UI |
| Concurrency | One synchronizer process active at a time; multiple concurrent writes not supported | Keep single-machine MVP explainable; use leases/fencing later |
| Changed fixture | Resume fails on SHA mismatch | Prevent applying a different batch sequence using stale checkpoint |
| Product absence | No implicit withdrawal | Batches are deltas, not guaranteed full snapshots |
| Price | Integer cents | Avoid floating-point money surprises |

## Intentional scope limits

These implemented boundaries remain intentional. Core CLI, persistence, sync, recovery and verification are complete; see `12_VERIFICATION.md`.

- **No live supplier connector.** The JSON adapter plus deterministic fault injection demonstrates behavior, not real HTTP/token refresh/pagination.
- **No autonomous background scheduler/retry queue.** Staff triggers start/resume manually.
- **No distributed coordination.** If multiple worker processes sync the same supplier at once, add a durable claim/lease with fencing token or equivalent enforcement before claiming production-grade concurrency.
- **No full issue remediation workflow.** Blocking malformed/conflicting input remains visible; the operator supplies a corrected **new update**, rather than editing history/patching an immutable fixture in place.
- **No declarative snapshot reconciliation.** Products absent from delta events remain as they were.
- **No authentication/permissions.** CLI runs locally against a local DB path.
- **No guaranteed extreme-scale performance.** One SQL transaction per batch is sufficient for fictional demo size; benchmark before production use.
- **No UI/public deployment**, intentionally.

## Questions a reviewer may ask

### Why not delete and rebuild the catalog?

A mid-update outage would erase the old catalog. Incremental per-product changes and small transactions preserve earlier data and the already committed part of the new update.

### Why is a `revision` required?

Batch arrival order alone can't prove freshness. We declare a contract with monotonically increasing revisions per product. Real supplier integrations would need to map their version/sequence semantics to this contract; arbitrary client arrival timestamps are not a safe replacement.

### Why can a withdrawn product return at v6?

The requirement only forbids **older** data restoring a withdrawn product. This MVP models a supplier's explicit higher-revision reactivation. If business rules require permanent withdrawals, change the pure reducer and tests.

### Why store tombstones for products never previously seen?

Because a withdrawal could arrive before a stale active event; without a tombstone, an old active record might later create the withdrawn product.

### Why do you record batch progress transactionally?

It guarantees that either all effects of the batch and its checkpoint persist or none do. After restart, there is no gap where progress incorrectly claims data was imported.

### Are writes exactly once?

We do **not** claim exactly-once network delivery. Sources may repeat messages. We achieve effectively-once *database effects* with idempotent reduction, unique product/update IDs, an applied-batch ledger and transactional checkpoints.

### What happens if the process crashes right before/after Commit?

SQLite guarantees all-or-nothing transaction effects. The next process reads the authoritative DB checkpoint and batch ledger. It never guesses from the previous process's memory or exit status.

### What if one event in a batch is bad?

Rollback that batch, persist a blocking issue, keep earlier committed batches, and do not report complete. This favors explicit correctness over silently dropping supplier events.

## Production improvements (not assignment requirements)

A real provider adapter with stable cursor or durable inbox, worker lease/fencing, richer manual issue resolution, backoff/retry limits, source signatures, backups/restore, metrics, structured logs, migrations exercised across upgrades, and large-dataset tests. Don't build these before completing acceptance tests.
