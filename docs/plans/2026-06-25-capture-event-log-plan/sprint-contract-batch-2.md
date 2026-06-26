# Batch 2 Sprint Contract

## Tasks

| ID | Subject | Type |
|----|---------|------|
| 004 | Payload parse & redaction (test) | test |
| 005 | Payload parse & redaction (impl) | impl |
| 006 | Capture service append-only & non-blocking (test) | test |
| 007 | Capture service append-only & non-blocking (impl) | impl |

## Acceptance Criteria

### Task 004: Payload parse & redaction (test) — Red

- [ ] Test file compiles; tests FAIL because impl is absent (not test bugs).
- [ ] Expanded `claudeHookPayload` parses the full PostToolUse payload (tool_input/tool_response/cwd/transcript_path/hook_event_name/permission_mode) into an `EventRecord` (architecture.md "Claude PostToolUse Payload Mapping").
- [ ] A secret token (e.g. AWS key) in the payload becomes a Typed Placeholder `[REDACTED:<rule-id>]`; raw value never stored.
- [ ] A sensitive file path (e.g. `.env`) is stored as a Redaction Digest (sha256 + length), not contents.
- [ ] An oversized payload is bounded (truncation flag + recorded `payload_size`); hash covers exactly the stored bytes.
- [ ] Malformed JSON does not error; interactive (no piped stdin) is a no-op for payload merge (falls back to flags).

### Task 005: Payload parse & redaction (impl) — Green

- [ ] `cmd/capture_payload.go` expanded; builds `EventRecord`; retains post-redaction raw bytes as `PayloadRaw`.
- [ ] Redaction component at `infrastructure/redact/` (path denylist -> Redaction Digest; compiled-once token regex set -> Typed Placeholder; size cap).
- [ ] All task-004 tests pass; `go build ./...`; `gofmt -l` clean.

### Task 006: Capture service append-only & non-blocking (test) — Red

- [ ] Test file compiles; tests FAIL because the append-only rewrite is absent.
- [ ] Capture records the observed payload verbatim as one Event (source `claude-code`, correct tool); no `git diff`/`hash-object` on the hot path.
- [ ] Edit-then-revert produces two Events (never a net `skipped`).
- [ ] On SQLITE_BUSY lock contention, capture skips + warns to stderr + returns nil (exit 0), never blocking.
- [ ] Uses fakes for `GraphRepository`/`EventHasher`.

### Task 007: Capture service append-only & non-blocking (impl) — Green

- [ ] `application/capture_service.go` rewritten to append-only: build `EventRecord` -> `repo.HeadHash` -> `hasher.Hash` -> `repo.AppendEvent`. **Removes the Batch-1 stop-gap** (diff-based delta + nil baseline) and the `DiffNameOnly`/`HashObject`/baseline remnants + `truncateDiff`/`parseDiffStat`/`maxDiffBytes`.
- [ ] `CaptureService` gains a `hasher graph.EventHasher` field; `cmd/capture.go` wires `infraGraph.NewSHA256Hasher()` into `NewCaptureService`.
- [ ] If the now-unused `baselineUpdates` param on `CreateActionBatch` has no remaining caller need, remove it and update call sites (otherwise document why kept).
- [ ] All task-006 tests pass; `go build ./...`; `gofmt -l` clean; `go test ./... -count=1` no regressions.

## Red-Green Pairs

| Test Task | Impl Task | Expected Red State | Expected Green State |
|-----------|-----------|--------------------|----------------------|
| 004 | 005 | Tests fail (no payload parse / redaction impl) | Pass after task 005 |
| 006 | 007 | Tests fail (capture still diff-based stop-gap) | Pass after task 007 |

## Evaluation Criteria Preview

The evaluator will apply `docs/retros/checklists/code-v1.md`:

| Item ID | Description |
|---------|-------------|
| CODE-VER-01 | All verification commands exit with code 0 |
| CODE-QUAL-01 | No TODO/FIXME/HACK/XXX/STUB/stub markers in produced files |
| CODE-QUAL-02 | No stub implementations (NotImplementedError, pass-only, ellipsis-only bodies) |

## Sign-off

- **Generator:** executing-plans
- **Status:** READY
- **Revision:** 0
