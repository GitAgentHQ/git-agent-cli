# Handoff State — Capture Event Log Execution

- **Branch:** `feat/capture-event-log` (off `feat/code-graph-sqlite`; capture subsystem pre-merge redesign)
- **Plan:** `docs/plans/2026-06-25-capture-event-log-plan/`
- **Design:** `docs/plans/2026-06-25-capture-event-log-design/` (architecture.md, bdd-specs.md, best-practices.md are the spec source)

## Completed task IDs

- 001, 002, 003 (Batch 1 — PASS). Event Log foundation: domain types, `events`/`event_files` schema, SHA-256 hasher + `AppendEvent`/`HeadHash`/`StreamEvents`. Full suite green incl. e2e.
- 004, 005, 006, 007 (Batch 2 — PASS). Payload parse + redaction; capture rewritten append-only. Full suite green incl. e2e.
- 008, 009, 010, 011 (Batch 3 — PASS). Outcome-event capture (Bash exit-code classify) + `graph verify` (4 ChainBreak kinds, exit 4). Full suite green.
- 012, 013, 014, 015 (Batch 4 — PASS). ProjectionRebuilder + `graph rebuild` (verify-first, deterministic); ReconcileService out-of-band net; timeline e2e restored. Full suite green.
- 016, 017, 018, 019 (Batch 5 — PASS). `graph provenance` (rename-aware + out-of-band) and `diagnose` (suspect-window ranking, verify-first, bounded LLM rerank port). Diagnose stub replaced; marker grep CLEAN repo-wide. Full suite green.
- 020 (Batch 6 — PASS). Scope-boundary regression gate: full suite green, marker grep clean, code-graph index verified byte-identical (DDL block, 6 query method bodies, indexer.go, AST/FTS5 layer all unchanged).

**ALL 20 TASKS COMPLETE.** Capture redesign fully implemented on `feat/capture-event-log`; build + vet + gofmt + full test suite (incl. e2e) green.

## Batch 5 outputs (for Batch 6)

- `application/{provenance_service,diagnose_service}.go` (+tests); `cmd/graph_provenance.go`; `cmd/diagnose.go` real impl (kept top-level `diagnose` to preserve `TestCapture_HiddenFromHelp`, delegates to `DiagnoseService`).
- `domain/graph`: +`FileChangeRow`; `GraphRepository` +`FileChanges`. `coChangeFailRepo` updated.
- `application.DiagnoseReranker` port exists; `--llm` is a no-op without a backend (design-allowed: no endpoint -> deterministic order final).
- Repo-wide marker grep is now CLEAN — no "stub"/TODO/etc. exemptions remain.

## Batch 4 outputs (for Batch 5+)

- `application/projection_service.go`: `ProjectionRebuilder` (verify-first -> ResetProjections -> StreamEvents fold). Session ID = `source:instance_id:firstSeq`; file changes parsed from `tool_input` (Edit/Write/MultiEdit) in `payload_raw`. `action_produces` left to existing `LinkActionsToCommit`.
- `application/reconcile_service.go`: `ReconcileService` Blind-Spot net; out-of-band Event IDs content-derived `oob-<sha>` so `events.event_id` UNIQUE makes reconcile idempotent. Wired at `graph rebuild` (reconcile-before-rebuild) and a best-effort commit-time seam in `cmd/commit.go`.
- `domain/graph`: +`EventFile` value type; `GraphRepository` +`ResetProjections`/`CreateEventFile`/`LatestAfterBlob`. (Concrete fake `coChangeFailRepo` must implement new interface methods — recurring friction; `fakeGraphRepository` embeds the interface so it's free.)
- `cmd/graph_rebuild.go`: `graph rebuild` attached to the `graph` parent.
- DIAGNOSE-STUB EXEMPTION: the pre-existing "stub" hits in `e2e/capture_timeline_test.go` are `TestDiagnose_StubMessage` for the `cmd/diagnose.go` stub. TASK 019 replaces that stub with the real `graph diagnose` — rework/rename `TestDiagnose_StubMessage` to real behavior, which ELIMINATES the CODE-QUAL-01 exemption. After Batch 5 there should be NO sanctioned "stub" markers left.
- Note: `ImpactService.Impact`/`ResolveRenames` are read-only reused by provenance/diagnose — reading the code index is allowed; the scope boundary forbids MODIFYING it.

## Batch 3 outputs (for Batch 4+)

- `cmd/graph.go` (new): `graph` PARENT command (wiring only). New subcommands (`graph rebuild`/`graph provenance`) attach here. `graph verify` already attached.
- `cmd/graph_verify.go` + `application/verify_service.go`: `VerifyService`; exit 4 via `pkg/errors.ErrChainIntegrity` on a ChainBreak; `--json`.
- `domain/graph/repository.go`: `VerifyChain` NOW on the `GraphRepository` interface (any concrete fake needs it; `fakeGraphRepository` embeds the interface so it's fine; `coChangeFailRepo` was updated).
- `infrastructure/graph/sqlite_repository.go`: `VerifyChain` + `loadChainRows`. Break-classification precedence INSERTED→DELETED→EDITED→REORDERED is load-bearing (INSERTED guarded by seq-contiguous).
- `application/outcome_classifier.go`: `ClassifyCommand`/`ExtractReportedExitCode`/`InferExitCode`. `capture_service.go` has a Bash Outcome branch; `CaptureRequest` gained transient `ToolResponse`.
- CHAIN-TEST FIXTURE NOTE: `seq` + `prev_hash` are folded into the canonical form, so you cannot build a ROW_REORDERED (or any tamper) fixture by post-hoc swaps — hand-compute valid hashes via direct INSERT where genesis-linkage order disagrees with stored seq order. Applies to reconciliation/provenance/diagnose tests that touch the chain.

## Cumulative modified files

- domain/graph/event.go (new): EventRecord, EventKind/EventSource (incl. out-of-band/unknown), EventHasher port, EventCursor, VerifyResult/ChainBreak/ChainBreakKind, GenesisHash
- domain/graph/repository.go: +AppendEvent/HeadHash/StreamEvents; -3 baseline methods; `VerifyChain` NOT yet on interface (lands task 011)
- domain/graph/session.go: -CaptureBaseline; SessionNode/ActionNode = projection DTOs
- infrastructure/graph/sqlite_client.go: +events/event_files DDL+indexes; -capture_baseline; CurrentSchemaVersion 1->2
- infrastructure/graph/sqlite_repository.go: real AppendEvent (BEGIN IMMEDIATE, explicit seq = MAX+1 inside txn, then hash), HeadHash, StreamEvents (+sqliteEventCursor); hasher field defaulting to NewSHA256Hasher(); -baseline impls; CreateActionBatch baseline branch removed (param `baselineUpdates` kept but unused for now)
- infrastructure/graph/sha256_hasher.go (new): canonical form = chain_version byte + LE seq/recorded_at + length-prefixed source/instance_id/kind/tool_name + LE exit-code sentinel + sha256(payload_raw)
- infrastructure/graph/{sha256_hasher_test.go, sqlite_repository_event_test.go, append_only_guard_test.go} (new tests)
- infrastructure/graph/sqlite_client_test.go: table-list updated
- application/capture_service.go: STOP-GAP — still diff-based (treats all changed files as deltas, passes nil baseline). FULL append-only rewrite is TASK 007.
- application/{capture_service_test.go, graph_index_test.go, graph_ensure_index_test.go}: baseline removed from mocks/tests
- e2e/capture_timeline_test.go: EndSessionLifecycle updated to new (post-end capture -> new session)

## Key decisions / cross-batch notes

- Scope boundary (HARD): capture subsystem only. Code-graph index (commits/files/authors/modifies/authored/co_changed/renames + AST + FTS5 + Impact/ResolveRenames) UNTOUCHED. Task 020 verifies.
- `VerifyChain` interface method deferred to task 011 (types exist now).
- Canonical form: hash exact stored payload bytes, never re-serialize (see sha256_hasher.go).
- seq assigned explicitly as MAX(seq)+1 inside BEGIN IMMEDIATE before hashing, so verify recomputes identically.
- One chain per repo; concurrent agents distinguished by `instance_id` at projection layer.
- **Task 007 must remove the capture_service.go STOP-GAP**: implement HeadHash -> hasher.Hash -> AppendEvent, add `hasher graph.EventHasher` field, wire `NewSHA256Hasher()` in cmd/capture.go, drop the diff/baseline remnants. Decide whether to drop the now-unused `baselineUpdates` param on `CreateActionBatch` (remove if no caller needs it).
- Checklist gotcha: CODE-QUAL-01 flags literal "stub" (case-insensitive) — avoid in code/comments. (Pre-existing "stub" hits in e2e/capture_timeline_test.go:11,24 reference the diagnose stub; leave them — they're handled in task 019.)
- Redaction component location: `infrastructure/redact/` (per task-005).

## Batch 2 outputs (for Batch 3+)

- `cmd/capture_payload.go`: full `claudeHookPayload` (session_id/transcript_path/cwd/hook_event_name/permission_mode/tool_name/tool_input/tool_response as RawMessage); `buildEventRecord(source,tool,instanceID,stdin,redact.Redactor) (EventRecord, ok)` — ok=false on empty/interactive/malformed; PayloadRaw = exact post-redaction bytes.
- `infrastructure/redact/` (new): `Redactor` iface + `NewRedactor()`; Layer A path denylist -> Redaction Digest `[REDACTED-DIGEST:sha256:<hex>:len=N]` on tool_input/tool_response content/old_string/new_string; Layer B compiled-once token regex (aws/github/slack/private-key) -> Typed Placeholder `[REDACTED:<rule-id>]`; size cap `maxPayloadBytes=512KiB` with `Result{Bytes,OrigSize,Truncated}`.
- `application/capture_service.go`: rewritten append-only. `NewCaptureService(repo, git, idGen, hasher graph.EventHasher)`. Flow: EndSession/nil-Event -> skip; else EventID via idGen, HeadHash -> hasher.Hash -> AppendEvent. `git` retained ONLY for cold paths (never called on hot path). Removed diff/baseline/`truncateDiff`/`parseDiffStat`/`maxDiffBytes`; deleted `application/diff_stat.go`+test. `excludeToolingPaths`/`endSession` removed. `sessionTimeoutMins` kept for projection/session logic (currently unreferenced).
- `domain/graph/event.go`: +`ErrChainBusy` sentinel.
- `domain/graph/session.go`: `CaptureRequest` +`Event *EventRecord`; `CaptureResult` now `{EventID,Seq,Source,CaptureMs,Skipped,Reason}` (dropped ActionID/SessionID/FilesChanged — those return when projections land).
- `domain/graph/repository.go` + `infrastructure/graph/sqlite_repository.go`: `CreateActionBatch` lost the unused `baselineUpdates` param (all callers passed nil); `AppendEvent` now maps SQLITE_BUSY (incl. extended codes, low-8-bits == 5) to `graph.ErrChainBusy` via `isBusyErr` (imports `modernc.org/sqlite` + `.../lib`).
- `cmd/capture.go`: wires `NewSHA256Hasher()` + builds Event via `buildEventRecord` (redaction applied) before `Capture`; ok=false -> flag-only no-op, never errors.

## Cross-batch deviation (e2e) — ACTION for Batch 3

Append-only capture no longer writes the `sessions`/`actions` projections synchronously, and the `ProjectionRebuilder` that repopulates them lands in task 012/013. The old `e2e/capture_timeline_test.go` asserted projection-derived `timeline` output and `CaptureResult.ActionID/SessionID` — incompatible with append-only. To keep the full suite green this batch, those two e2e tests were rewritten to the append-only contract:
- `TestCapture_Timeline_System` -> `TestCapture_AppendsObservedPayload` (payload on stdin -> seq 1 then seq 2; edit-then-revert = two events; exit 0). Added `gitAgentStdin` helper in `e2e/helpers_test.go`.
- `TestCapture_EndSessionLifecycle` -> `TestCapture_EndSessionIsNonBlocking` + `TestCapture_NoPayloadIsNonBlockingNoOp`.
- `TestDiagnose_StubMessage` + `TestCapture_HiddenFromHelp` untouched (pre-existing "stub" refs at capture_timeline_test.go:11,24 + helpers_test.go:51 are the diagnose stub, still handled in task 019).
**Batch 3 must restore timeline e2e coverage** once `graph rebuild`/projection exists (task 012/013), and update task-020's "run-only" e2e expectation accordingly.

## Recurring failure patterns

- CODE-QUAL-01 vs. the diagnose-stub exemption (Batch 2 audit): the binary `stub\b`
  grep in code-v1.md matches three PRE-EXISTING, exempted lines describing the
  task-019 diagnose stub — `e2e/capture_timeline_test.go:11,24` (the
  `TestDiagnose_StubMessage` name + its "stub message" assertion) and
  `e2e/helpers_test.go:51` (a comment on `gitAgentSeparated`). All three predate
  Batch 2 (git blame: 7024d42 / a71b87e) and appear in zero added (`+`) lines of
  this batch. Per handoff line 35 these stay until task 019 reworks diagnose.
  RETROSPECTIVE ACTION: amend code-v1.md CODE-QUAL-01 to exclude the `stub` token
  inside identifiers/test-contract prose (or scope the grep to non-test files),
  so the deterministic check stops flagging an explicitly-sanctioned exemption.
  Do NOT rename `TestDiagnose_StubMessage` here — that is task-019's contract.
