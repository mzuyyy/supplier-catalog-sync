# 03 — Supplier fixtures, schema and audit contract

## 1. Supplier update fixture (JSON)

Each fixture represents one immutable supplier update. It is a sequence of ordered **batch envelopes**; product events inside/across envelopes can have arbitrary revision order. `batch_index` is sequential and zero-based. `is_last=true` is required **only** on the final batch; `declared_batch_count` must equal the number of batch envelopes. A fixture without a valid terminator must be rejected or blocked, never marked complete.

```json
{
  "supplier_id": "demo-supplier",
  "update_id": "update-2026-001",
  "declared_batch_count": 3,
  "batches": [
    {
      "batch_index": 0,
      "is_last": false,
      "events": [
        { "product_id": "P100", "revision": 2, "status": "active", "name": "Mouse", "price_cents": 2900 },
        { "product_id": "P101", "revision": 5, "status": "withdrawn" }
      ]
    },
    {
      "batch_index": 1,
      "is_last": false,
      "events": [
        { "product_id": "P100", "revision": 1, "status": "active", "name": "Old Mouse", "price_cents": 1000 },
        { "product_id": "P102", "revision": 1, "status": "active", "name": "Stand", "price_cents": 4500 }
      ]
    },
    {
      "batch_index": 2,
      "is_last": true,
      "events": [
        { "product_id": "P100", "revision": 3, "status": "active", "name": "Mouse Pro", "price_cents": 3200 },
        { "product_id": "P101", "revision": 3, "status": "active", "name": "Stale Cable", "price_cents": 900 }
      ]
    }
  ]
}
```

**Required checks:** nonempty supplier/update/product IDs, revision > 0, status in `{active,withdrawn}`, active requires nonempty `name` and `price_cents >= 0` (integer), valid indices/no gaps, exactly one final `is_last`, correct batch count, and no unknown fields if strict decoding is chosen. Withdrawn events must omit name and price; **reject them if either field is supplied** to remove ambiguity.

**Source identity:** persist `source_path` and SHA-256 of the original fixture file. On resume, verify same SHA-256 before using batch indices. On repeated `sync start` with the same `(supplier_id,update_id)`, return the existing job; if content hash differs, report source identity conflict, never mutate prior job silently.

**Canonical event digest:** SHA-256 of the stable normalized tuple `(product_id, revision, status, name, price_cents)` with absent name/price explicitly represented as null. Digest determines whether same revision has identical content. Do NOT include batch location, current timestamp or local arrival sequence in this digest. Canonical JSON of a typed struct is acceptable if the exact field set is fixed.

## 2. Database tables (logical schema)

Names/types are guidance; adapt physical DDL while keeping the invariants.

### `products`

| Field | Meaning |
|---|---|
| `supplier_id TEXT NOT NULL` | Composite identity |
| `product_id TEXT NOT NULL` | Composite identity |
| `revision INTEGER NOT NULL` | Newest supplier revision retained |
| `status TEXT NOT NULL` | `active` / `withdrawn` |
| `name TEXT NULL` | Latest known name; may be null for unknown withdrawal |
| `price_cents INTEGER NULL` | Latest known price; null for unknown withdrawal |
| `event_digest TEXT NOT NULL` | Digest of newest supplier event, incl. tombstone |
| `updated_at TEXT NOT NULL` | Timestamp of committed local change |

Primary key `(supplier_id, product_id)`; check revision > 0, status enum, and nonnegative price if not null. Keep name/price on withdrawal of a **known** product for audit/context, but use event digest from the withdrawal event (not retained old fields). For unknown withdrawn product, insert a tombstone with null fields.

### `sync_jobs`

| Field | Meaning |
|---|---|
| `job_id TEXT PRIMARY KEY` | Generated stable ID (UUID or readable unique string) |
| `supplier_id`, `update_id` | Source identity, UNIQUE pair |
| `source_path`, `source_sha256` | Immutable source retrieval information |
| `declared_batch_count INTEGER` | Trusted manifest count |
| `next_batch_index INTEGER DEFAULT 0` | Next uncommitted batch, 0-based |
| `status TEXT` | `PENDING/RUNNING/INTERRUPTED/NEEDS_ATTENTION/COMPLETED` |
| `created_count`, `updated_count`, `withdrawn_count`, `reactivated_count`, `duplicate_count`, `stale_count` | Counters updated only in same transaction as a completed batch |
| `started_at`, `finished_at`, `updated_at` | Lifecycle timestamps |
| `last_error TEXT NULL` | Concise human-facing explanation |

Constraint `0 <= next_batch_index <= declared_batch_count`.

### `sync_batches`

Unique `(job_id, batch_index)`; fields `batch_digest`, `committed_at`, optional counts. A committed batch ledger is written **in the same transaction** as products, change events and `next_batch_index` advancement. On resume, batch ledger and checkpoint must agree; treat disagreement as storage corruption, never auto-skip.

### `sync_changes`

Append-only committed effects only. Fields: `id`, `job_id`, `batch_index`, `supplier_id`, `product_id`, `revision`, `change_type` (`created`,`updated`,`withdrawn`,`reactivated`), `old_record_json`, `new_record_json`, `created_at`. Uniqueness guard e.g. `(job_id, product_id, revision)` for applied changes. Rejected, duplicated or stale events do **not** create product change entries. Include old/new values sufficient to explain what changed.

### `sync_issues`

Fields: `id`, `job_id`, `batch_index NULL`, `product_id NULL`, `code`, `message`, `blocking BOOLEAN`, `status` (`open`,`resolved`), `created_at`, `resolved_at NULL`. Codes: `SUPPLIER_UNAVAILABLE`, `INVALID_MANIFEST`, `INVALID_RECORD`, `REVISION_CONFLICT`, `SOURCE_CHANGED`, `STORAGE_ERROR` (as applicable).

An outage creates an open issue; when the affected batch eventually **commits**, resolve earlier outage issues for that batch in the same transaction. Permanent invalid/conflicting data remains open. Prevent repeated identical open issues flooding the table (dedupe by job/batch/product/code while open, or equivalent).

## 3. Important edge semantics

- **Same revision and same digest**: duplicate → increment duplicate counter once per committed input event, no product/change update.
- **Same revision and different digest**: conflict → current batch rollback, open blocking issue, `NEEDS_ATTENTION`.
- **Older revision**: stale → ignore, increment stale counter once per committed input event.
- **Newer withdrawn**: preserve tombstone; if unknown, create tombstone (counts as `withdrawn`, not `created`); if already withdrawn with newer revision, update tombstone revision and count `withdrawn`.
- **Newer active after withdrawn**: allowed as explicit supplier reactivation; count `reactivated` and log event. Older active remains blocked by revision comparison.
- **Active event with identical fields but higher revision**: update revision/digest; audit event counts as `updated` because source version changed (be consistent).
- **No missing-product deletion**: absence from update is not a withdrawal.

## 4. Fixture inventory for implementation

Create at least these files:

1. `fixtures/updates/basic.json` — >=3 batches, new products, modifications, withdraw, duplicate and stale records; predictable final state.
2. `fixtures/updates/out_of_order.json` — v3 then v1 then v2; stale activation after withdrawal.
3. `fixtures/updates/conflict.json` — same product and same revision with different content, triggers `NEEDS_ATTENTION`.
4. `fixtures/updates/invalid.json` — malformed/invalid event; triggers visible issue.
5. `fixtures/updates/reactivate.json` — newer active revision after withdrawal; proves deliberate policy.

Keep all fictional values and expected final results in tests.
