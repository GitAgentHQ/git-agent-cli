# Batch 1 Sprint Contract

## Tasks

| ID | Subject | Type |
|----|---------|------|
| 001 | Foundation: event domain types, schema, repository ports | setup |
| 002 | Hash chain & append (test) | test |
| 003 | Hash chain & append (impl) | impl |

## Acceptance Criteria

### Task 001: Foundation (setup)

- [ ] `domain/graph/event.go` created with `EventRecord`, `EventKind`/`EventSource` string types + consts (incl. `out-of-band`/`unknown`), `EventHasher` port, and audit-surface types `VerifyResult`/`ChainBreak`/`ChainBreakKind`.
- [ ] `events` + `event_files` DDL added to `infrastructure/graph/sqlite_client.go` `schemaStatements`; `CurrentSchemaVersion` bumped; `capture_baseline` DDL removed.
- [ ] `CaptureBaseline` value object removed from `domain/graph/session.go`.
- [ ] `GraphRepository` gains `AppendEvent`, `HeadHash`, `StreamEvents`; the three `Get/Update/CleanupCaptureBaseline` methods removed. (See [AUTO-RESOLVED] below re: `VerifyChain`.)
- [ ] Baseline method impls + the baseline branch of `CreateActionBatch` removed from `infrastructure/graph/sqlite_repository.go`.
- [ ] `go build ./...` compiles; `go vet ./domain/... ./infrastructure/...` clean.

- [ ] **[AUTO-RESOLVED]** task-001 lists `VerifyChain` among the new interface methods, but its full implementation + tests land in task 011 (Batch 3). Applied interpretation: **define the `VerifyResult`/`ChainBreak`/`ChainBreakKind` types now, but DEFER adding the `VerifyChain` method to the `GraphRepository` interface until task 011.** This keeps the SQLite repo compiling in Batch 1 without any placeholder/marker body (avoids CODE-QUAL-01/02 violations). `AppendEvent`/`HeadHash`/`StreamEvents` are added now and fully implemented in task 003.

### Task 002: Hash chain & append (test) — Red

- [ ] Test file compiles; tests FAIL because impl is absent (not due to test bugs).
- [ ] First Event gets `seq` 1 and genesis `prev_hash` (64 hex zeros); subsequent Events chain `prev_hash` = prior `this_hash`.
- [ ] `this_hash` covers `seq`, `recorded_at`, `source`, `tool_name`, and the payload bytes (canonical-form determinism: same input → same hash; field change → different hash).
- [ ] Ground truth is stored without invoking `git diff`/`git hash-object` on the hot path.
- [ ] Append-only guard: a test asserts no production path issues UPDATE/DELETE on `events`.

### Task 003: Hash chain & append (impl) — Green

- [ ] `infrastructure/graph/sha256_hasher.go` implements `EventHasher` + canonical form (chain_version byte + fixed-order length-prefixed scalars + `sha256(payload_raw)`).
- [ ] `AppendEvent` (BEGIN IMMEDIATE single-writer, assigns `seq` + `this_hash`), `HeadHash`, `StreamEvents` implemented in the SQLite repo.
- [ ] All task-002 tests pass (exit 0); `go build ./...`; `gofmt -l` reports no files.
- [ ] No regressions in the existing suite.

## Red-Green Pairs

| Test Task | Impl Task | Expected Red State | Expected Green State |
|-----------|-----------|--------------------|----------------------|
| 002 | 003 | Tests run, assertions fail (no `AppendEvent`/hasher impl) | Tests pass after task 003 |

## Evaluation Criteria Preview

The evaluator will apply `docs/retros/checklists/code-v1.md`:

| Item ID | Description |
|---------|-------------|
| CODE-VER-01 | All verification commands exit with code 0 |
| CODE-QUAL-01 | No TODO/FIXME/HACK/XXX/STUB/stub markers in produced files |
| CODE-QUAL-02 | No stub implementations (NotImplementedError, pass-only, ellipsis-only bodies) |

Note: CODE-QUAL-01 matches `stub` case-insensitively — produced Go files (and comments) must avoid the literal word "stub" and all listed markers.

## Sign-off

- **Generator:** executing-plans
- **Status:** READY
- **Revision:** 0
