# 01 — Product requirements and acceptance criteria

## Source assignment (meaning-preserving summary)

A shop has a durable local supplier catalog. Staff initiate catalog updates and can tell if the update finished, what changed, and what needs attention. Supplier updates contain multiple batches of new/modified/withdrawn products; events may repeat or arrive out of order. Importing the same update twice must not duplicate products. Old data cannot replace new data or resurrect a withdrawn product. Supplier outages and process interruption must preserve the existing catalog. A restart must permit resuming without losing completed work; incomplete work cannot be reported complete. Fictional supplier data is sufficient. Go is mandatory. A visual interface and public deployment are not required. Submission requires run instructions, assumptions and unfinished work.

## Personas and user journeys

**Staff operator**

1. Runs `sync start` against an identified supplier update.
2. Sees a job ID and progress/status.
3. Can list catalog and committed product changes.
4. If unavailable/terminated, can restart the binary and run `sync resume --job ...`.
5. Can list unresolved issues, with reason and batch/product context.
6. Must never see `COMPLETED` when batches remain or blocking issues exist.

**Reviewer/interviewer**

1. Runs on a local machine without a real supplier credential.
2. Replays the same source and observes no duplicate products/change events.
3. Shows older and out-of-order updates being ignored.
4. Shows withdrawal tombstones resisting stale activation.
5. Stops the process and restarts, verifies checkpoint/data preservation.
6. Understands code and can make a related change without AI.

## Must-have acceptance criteria

| ID | Criterion | Proof |
|---|---|---|
| AC-01 | Disk-persisted catalog survives restarting the program | Integration test: close/reopen DB |
| AC-02 | New revision for unknown product creates one row | Test + demo |
| AC-03 | Strictly higher revision updates existing product | Table-driven tests |
| AC-04 | Older revision cannot overwrite newer fields | Out-of-order fixture/test |
| AC-05 | Identical same-revision event is no-op; conflicting same-revision event is an issue | Unit + integration tests |
| AC-06 | Withdrawn events persist tombstones, even for unknown products | Test unknown withdrawal |
| AC-07 | Older active event cannot reactivate a withdrawn product | Test sequence withdrawn v5, active v3 |
| AC-08 | Importing an already-completed update is idempotent, with no new change events/counters | Repeat start and compare |
| AC-09 | Failure in a batch cannot partly commit that batch | Fault-injection test |
| AC-10 | Completed earlier batches persist after failure | Reopen disk DB after interruption |
| AC-11 | Restart + resume continues at committed checkpoint | New program instance test |
| AC-12 | A job can't be `COMPLETED` without end marker + all batches committed + zero open blocking issues | Completion-gate tests |
| AC-13 | Staff can inspect changes, job status and open issues using CLI | CLI integration tests |
| AC-14 | README gives verified run/test/demo commands and explicitly names assumptions and incomplete work | Manual review |

## Nonfunctional goals

- Reliability and clarity over throughput. A few hundred fictional products and 3–10 batches are enough.
- Local-only: no external credentials, cloud services or UI.
- Deterministic fixture behavior and failure injection.
- Understandable package boundaries, straightforward error handling and tests.
- No destructive `DELETE FROM products` before sync.
- Useful logs to stderr, machine-readable optional `--json` output only if easy.

## Explicit non-goals

- Product ordering, billing, payments, employee accounts or admin login.
- Live supplier API integration, customer-facing website, public deployment.
- Distributed workers, multi-instance concurrent writers, change-data-capture pipeline.
- Full event sourcing, granular manual issue-resolution UI, automatic scheduling.
- Global inventory reconciliation from absence in a partial batch; absence does **not** imply withdrawal.

## Definition of success

The demo imports a multi-batch fixture, shows a durable catalog and change history, replays old/duplicate records without mutation, simulates failure after committed work, reopens the same SQLite database in a new process, resumes and reaches completion only after all batches commit. A separate conflicting input produces a visible unresolved issue and a non-success job state.
