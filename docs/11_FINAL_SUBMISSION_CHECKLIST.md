# 11 — Final submission checklist

Automated/product checks completed on 2026-10-22; see `12_VERIFICATION.md`. Interview readiness below remains for the human developer. Race detection was attempted but the environment lacks CGO/C compiler support.

## Functionality

- [x] `go build ./cmd/catalogsync` succeeds.
- [x] Fresh DB imports fictional supplier fixture and persists products.
- [x] CLI supports start, resume, status, change history, issues and catalog browsing.
- [x] New/updated/withdrawn events affect catalog correctly.
- [x] Duplicate same-revision events are no-ops and conflicting ones are reported.
- [x] Older records cannot overwrite newer records or resurrect withdrawn products.
- [x] A failed batch leaves no partial product/history/counter/checkpoint writes.
- [x] Supplier outage preserves existing catalog and returns non-success.
- [x] A force-killed process can be replaced by a new process which resumes to completion.
- [x] Incomplete/blocked job never displays `COMPLETED`.

## Quality

- [x] Unit tests and SQLite integration tests run on fresh file-backed DBs.
- [x] `go test ./...` and `go vet ./...` pass (report if not).
- [x] All source files `gofmt`-formatted.
- [x] No hardcoded absolute local paths in executable code or README.
- [x] No secrets, private credentials, generated `.db` files, or binaries committed unnecessarily.
- [x] `.gitignore` excludes `.db`, SQLite WAL/SHM, binaries and temporary demo artifacts.
- [x] Dependency versions pinned in `go.mod` and `go.sum` and included in the Git repository.

## Documentation

- [x] README contains **real** prerequisites and exact working commands.
- [x] README explains data model, transaction/checkpoint strategy and version/tombstone policy.
- [x] README shows a reproducible failure/resume demo and known limitations.
- [x] README does not claim production-grade concurrency or real supplier integration.
- [x] Assumptions distinguish original requirements from chosen mock-supplier behaviors.
- [x] Unfinished work is a truthful list, not hidden TODOs.

## Interview readiness

- [ ] I can explain `Decide` without reading code line by line.
- [ ] I can identify the exact transaction where batch checkpoint advances.
- [ ] I can identify the database query checking an already-applied revision.
- [ ] I can explain how the next process recovers after `SIGKILL`.
- [ ] I can add a read-only CLI filter/test by myself without generative AI.
