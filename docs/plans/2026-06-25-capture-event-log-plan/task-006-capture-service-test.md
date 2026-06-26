# Task 006: Capture service append-only & non-blocking (test)

**type**: test
**depends-on**: ["001"]

## Files
- modify: `application/capture_service_test.go` — replace the diff-reconstruction
  tests with append-only-flow tests against fake `GraphRepository` + fake
  `EventHasher`.
- modify (if needed): `application/mock_graph_git_client_test.go` — the git fake
  must no longer be exercised on the hot path; add a fake `GraphRepository` and a
  fake `EventHasher` (new test doubles) capturing `AppendEvent`/`HeadHash` calls.

## BDD Scenario(s)
```gherkin
Scenario: Capture records the observed tool payload verbatim
  Given a Claude PostToolUse payload with tool_name "Edit" editing "src/main.go"
  When capture is invoked with the payload on stdin
  Then a new Event is appended with source "claude-code", kind "tool", tool_name "Edit"
  And the Event payload_raw matches the post-redaction bytes received
  And the Event prev_hash equals the prior chain head this_hash
  And the hook exits 0 within the capture budget

Scenario: Edit-then-revert is preserved as two Events
  Given an Edit payload that changes "a.go" from X to Y
  And a following Edit payload that changes "a.go" from Y back to X
  When capture is invoked for each payload
  Then the Event Log contains two Events
  And neither capture is reported as skipped

Scenario: Capture never blocks on write-lock contention
  Given another writer holds the write lock beyond the busy timeout
  When capture is invoked
  Then capture skips appending the Event
  And it writes a warning to stderr
  And it exits 0 without blocking the agent
```

## What to implement

RED tests for the rewritten `CaptureService.Capture` append-only flow. No
production code; the new constructor signature (hasher arg) and append-only flow
do not exist yet, so these must fail to compile/run for the right reason.

### Fakes (isolate DB/git/network)

- `fakeGraphRepository`: records `AppendEvent` calls (returns an `EventRecord`
  with assigned `Seq`/`ThisHash`), serves a programmable `HeadHash`. Has a
  `busy bool` (or an error to return) so `AppendEvent` can simulate
  `SQLITE_BUSY` lock contention without a real DB.
- `fakeEventHasher`: deterministic `Hash(prevHash, e) string` (e.g. a fixed-format
  string of inputs) so `this_hash`/`prev_hash` linkage is assertable without
  SHA-256 details.

No `git` calls expected on the hot path — assert the git fake's
`DiffNameOnly`/`HashObject`/`GetCaptureBaseline` are **never** invoked (ground-truth
retained, architecture.md System Overview invariant 2).

### Behavior under test

The new flow per architecture.md "application/" + Hash Chain: build `EventRecord`
from the request → `repo.HeadHash(ctx)` → `hasher.Hash(prev, e)` →
`repo.AppendEvent(ctx, e)`. NO `DiffNameOnly`/`HashObject`/`GetCaptureBaseline`,
no `GetCaptureBaseline`/baseline delta, no `parseDiffStat`/`truncateDiff`.

1. **Verbatim append.** Given an Edit `EventRecord` (source `claude-code`, kind
   `tool`, tool_name `Edit`, `PayloadRaw` = the post-redaction bytes), assert
   `AppendEvent` received exactly that record with `PrevHash` == the head returned
   by `HeadHash` and `ThisHash` == `hasher.Hash(prev, e)`. Result is not skipped;
   no error.
2. **Edit-then-revert = two Events.** Two successive captures (X→Y then Y→X) each
   call `AppendEvent` once; two events recorded; neither result `Skipped`
   (FR8 — capture is unconditional per hook call; there is no net-diff path).
3. **Lock-contention non-blocking.** When `AppendEvent` returns `SQLITE_BUSY`,
   `Capture` returns `nil` error (or a skipped result the cmd layer maps to exit 0),
   writes a warning to stderr, and does not block (FR13, best-practices.md §2.1
   "silent skip on contention"). Assert the warning text and that exit-0 semantics
   hold — the agent is never blocked.

## Steps
1. Read architecture.md "application/" + "Hash Chain" (`AppendEvent`/`HeadHash`
   signatures) and best-practices.md §2.1.
2. Read existing `application/capture_service_test.go` + `mock_graph_git_client_test.go`
   on the branch to match style; add `fakeGraphRepository` and `fakeEventHasher`.
3. Delete/rewrite the diff-reconstruction test cases; write the three scenario
   tests above referencing the new `NewCaptureService(repo, git, idGen, hasher)`
   signature (task-007) and `domain/graph.EventRecord`/`EventHasher` (task-001).
4. Assert no git-client hot-path call occurs.

## Verification
- `go test ./application/...` — RED: fails to compile (new constructor arg /
  append-only flow absent) or fails assertions. Confirm each failure targets the
  missing append-only flow, not an unrelated symbol.
