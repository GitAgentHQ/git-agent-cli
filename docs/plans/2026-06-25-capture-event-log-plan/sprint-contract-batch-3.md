# Batch 3 Sprint Contract

## Tasks

| ID | Subject | Type |
|----|---------|------|
| 008 | Outcome events (test) | test |
| 009 | Outcome events (impl) | impl |
| 010 | graph verify chain integrity (test) | test |
| 011 | graph verify chain integrity (impl) | impl |

## Acceptance Criteria

### Task 008: Outcome events (test) — Red

- [ ] Test file compiles; tests FAIL because impl is absent.
- [ ] A Bash payload running a test command with `tool_response` exit code 1 produces an Outcome Event (`kind = outcome`, `is_test = true`, `exit_code = 1`, `exit_code_source = "reported"`).
- [ ] When `tool_response` has no explicit exit field but output indicates failure, `exit_code` is non-zero and `exit_code_source = "inferred"`.
- [ ] A non-test Bash command records an Outcome Event with `is_test = false` and `is_build = false`.

### Task 009: Outcome events (impl) — Green

- [ ] Deterministic Bash test/build command classifier (`go test`/`make test`/`go build`/`pnpm test`/`pytest`/`cargo test`/...); `test_name` from `-run`/package where present (architecture.md "Outcome Event capture").
- [ ] `exit_code` from `tool_response` when present (`reported`) else inferred from failure markers (`inferred`, down-weighted later). Compound commands -> aggregate exit (honest limit). Parse failure DROPS the outcome event, never fabricates.
- [ ] Wired into the append-only capture path (Batch-2 `buildEventRecord`/`EventRecord` outcome fields). All task-008 tests pass; build + gofmt clean.

### Task 010: graph verify (test) — Red

- [ ] Test file compiles; tests FAIL because `VerifyChain` is absent.
- [ ] Untouched chain -> `VerifyResult.Status == "ok"`.
- [ ] Direct row mutation -> first `ChainBreak.Kind == ROW_EDITED`.
- [ ] Deleted row (seq gap) -> `ROW_DELETED`.
- [ ] Inserted unreachable row -> `ROW_INSERTED`.
- [ ] Reordered linkage vs seq -> `ROW_REORDERED`.
- [ ] Uses a real temp SQLite db; mutates rows directly.

### Task 011: graph verify (impl) — Green

- [ ] **Add `VerifyChain` to the `GraphRepository` interface** (deferred from task 001) and implement it in `infrastructure/graph/sqlite_repository.go` (recompute `this_hash`; track `expected_prev`; genesis linkage walk vs `seq` order -> classify break per architecture.md "Audit Surface" table). Use the canonical `VerifyResult`/`ChainBreak`/`ChainBreakKind` types from `domain/graph/event.go`.
- [ ] `application/verify_service.go` + `cmd/graph_verify.go` (exit code 4 on break; `--json` output).
- [ ] All task-010 tests pass; `go build ./...`; `gofmt -l` clean; `go test ./... -count=1` no regressions.

## Red-Green Pairs

| Test Task | Impl Task | Expected Red State | Expected Green State |
|-----------|-----------|--------------------|----------------------|
| 008 | 009 | Tests fail (no outcome capture) | Pass after task 009 |
| 010 | 011 | Tests fail (no VerifyChain) | Pass after task 011 |

## Evaluation Criteria Preview

The evaluator will apply `docs/retros/checklists/code-v1.md`:

| Item ID | Description |
|---------|-------------|
| CODE-VER-01 | All verification commands exit with code 0 |
| CODE-QUAL-01 | No TODO/FIXME/HACK/XXX/STUB/stub markers in produced files |
| CODE-QUAL-02 | No stub implementations (NotImplementedError, pass-only, ellipsis-only bodies) |

Note: the only sanctioned `stub` exemptions are the pre-existing diagnose-stub references in `e2e/capture_timeline_test.go` + `e2e/helpers_test.go` (task 019). Do NOT touch those; introduce zero new markers.

## Sign-off

- **Generator:** executing-plans
- **Status:** READY
- **Revision:** 0
