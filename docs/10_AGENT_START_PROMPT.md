# 10 — Copy/paste prompt for the coding agent

Historical starter prompt retained for traceability. The implementation now exists; see README and `12_VERIFICATION.md` for current verified behavior.

Paste the following text into Codex / OpenCode / another coding agent **after placing this package's Markdown files at repository root**.

---

You are the lead Go engineer for this take-home assignment. Build a **fully runnable and testable** local supplier catalog synchronization product.

First read `AGENTS.md` and every linked document in the exact prescribed order. Treat `docs/04_SYNC_ALGORITHM.md` as a correctness contract. The current README is a **blueprint**, not evidence of a working product.

Do the implementation in this repo, not just a plan. Use Go CLI + embedded SQLite as specified. Keep the code small enough that I can explain every package and make a related change in an interview without AI. Do not build a frontend, distributed queue or cloud deployment.

Work in ordered milestones from `docs/07_IMPLEMENTATION_PLAN.md`:
1. Establish module and source fixtures.
2. Build durable schema and pure revision/tombstone reducer.
3. Build atomic per-batch sync with journal, checkpoint and failure handling.
4. Implement restart/resume using persisted source identity.
5. Implement staff CLI commands for start, status, changes, issues, catalog, resume.
6. Add unit/integration tests, including out-of-order/duplicates, rollback and a new-process restart/hard-kill demonstration.
7. Run the exact relevant build/test commands, fix failures and replace blueprint README with accurate run/test/demo/assumptions/unfinished-work documentation.

Required tricky cases:
- duplicate and out-of-order events;
- same revision with different data -> blocking issue;
- unknown-product withdrawal -> tombstone;
- stale active event after withdrawal -> no resurrection;
- a source fetch failing after earlier batches committed;
- `SIGKILL` leaving job `RUNNING` while the next process resumes from durable checkpoint;
- never show `COMPLETED` for missing batches or unresolved blocking issues.

Make important design choices explicit and update docs when deviating. Do not introduce complex abstractions unless they are needed. Never claim success without running tests. Update the task checklist as you progress. If time is limited, ship a smaller complete product with passing correctness tests rather than a half-finished larger one.

When finished, report:
- architecture and key invariants in 8–12 lines;
- exact commands executed and results;
- meaningful files changed;
- any genuine unfinished requirements or risk;
- a 5-minute reproduction sequence for happy path, outage/resume and conflict;
- two easy interview changes I can practice without AI.

---

## Optional second-pass review prompt

After implementation, paste this separately:

> Review this Go code as a strict take-home interviewer. Read `docs/01_REQUIREMENTS.md`, `docs/04_SYNC_ALGORITHM.md`, and `docs/06_TEST_PLAN.md`. Search for incorrect revision handling, stale withdrawal resurrection, idempotency mistakes, checkpoint/product writes outside the same SQL transaction, false-complete states, source hash mismatches, and missing close/reopen integration tests. Run the test suite and a clean-db CLI demo. Fix discovered bugs. Keep changes minimal and update README with only verified claims. Explain remaining risks instead of masking them.
