CREATE TABLE products (
 supplier_id TEXT NOT NULL,
 product_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision > 0),
 status TEXT NOT NULL CHECK(status IN ('active','withdrawn')),
 name TEXT,
 price_cents INTEGER CHECK(price_cents IS NULL OR price_cents >= 0),
 event_digest TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(supplier_id,product_id),
 CHECK(status != 'active' OR (name IS NOT NULL AND length(trim(name)) > 0 AND price_cents IS NOT NULL))
);
CREATE TABLE sync_jobs (
 job_id TEXT PRIMARY KEY,
 supplier_id TEXT NOT NULL,
 update_id TEXT NOT NULL,
 source_path TEXT NOT NULL,
 source_sha256 TEXT NOT NULL,
 declared_batch_count INTEGER NOT NULL CHECK(declared_batch_count > 0),
 next_batch_index INTEGER NOT NULL DEFAULT 0 CHECK(next_batch_index >= 0 AND next_batch_index <= declared_batch_count),
 status TEXT NOT NULL CHECK(status IN ('PENDING','RUNNING','INTERRUPTED','NEEDS_ATTENTION','COMPLETED')),
 created_count INTEGER NOT NULL DEFAULT 0 CHECK(created_count >= 0),
 updated_count INTEGER NOT NULL DEFAULT 0 CHECK(updated_count >= 0),
 withdrawn_count INTEGER NOT NULL DEFAULT 0 CHECK(withdrawn_count >= 0),
 reactivated_count INTEGER NOT NULL DEFAULT 0 CHECK(reactivated_count >= 0),
 duplicate_count INTEGER NOT NULL DEFAULT 0 CHECK(duplicate_count >= 0),
 stale_count INTEGER NOT NULL DEFAULT 0 CHECK(stale_count >= 0),
 started_at TEXT NOT NULL,
 finished_at TEXT,
 updated_at TEXT NOT NULL,
 last_error TEXT,
 UNIQUE(supplier_id,update_id),
 CHECK(status != 'COMPLETED' OR (next_batch_index = declared_batch_count AND finished_at IS NOT NULL))
);
CREATE TABLE sync_batches (
 job_id TEXT NOT NULL REFERENCES sync_jobs(job_id),
 batch_index INTEGER NOT NULL CHECK(batch_index >= 0),
 batch_digest TEXT NOT NULL,
 committed_at TEXT NOT NULL,
 PRIMARY KEY(job_id,batch_index)
);
CREATE TABLE sync_changes (
 id INTEGER PRIMARY KEY,
 job_id TEXT NOT NULL REFERENCES sync_jobs(job_id),
 batch_index INTEGER NOT NULL CHECK(batch_index >= 0),
 supplier_id TEXT NOT NULL,
 product_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision > 0),
 change_type TEXT NOT NULL CHECK(change_type IN ('created','updated','withdrawn','reactivated')),
 old_record_json TEXT NOT NULL,
 new_record_json TEXT NOT NULL,
 created_at TEXT NOT NULL,
 UNIQUE(job_id,product_id,revision),
 FOREIGN KEY(supplier_id,product_id) REFERENCES products(supplier_id,product_id)
);
CREATE TABLE sync_issues (
 id INTEGER PRIMARY KEY,
 job_id TEXT NOT NULL REFERENCES sync_jobs(job_id),
 batch_index INTEGER,
 product_id TEXT,
 code TEXT NOT NULL,
 message TEXT NOT NULL,
 blocking INTEGER NOT NULL CHECK(blocking IN (0,1)),
 status TEXT NOT NULL CHECK(status IN ('open','resolved')),
 created_at TEXT NOT NULL,
 resolved_at TEXT
);
CREATE UNIQUE INDEX sync_issues_open ON sync_issues(job_id,coalesce(batch_index,-1),coalesce(product_id,''),code) WHERE status='open';
CREATE INDEX sync_changes_job ON sync_changes(job_id,batch_index,id);
CREATE INDEX sync_issues_job ON sync_issues(job_id,status,id);
