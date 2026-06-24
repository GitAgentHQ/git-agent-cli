# Task 007: Capture service append-only & non-blocking (impl)

**type**: impl
**depends-on**: ["006", "003", "005"]

## Files
- modify: `application/capture_service.go` — gut the diff/baseline path; new
  append-only flow; add a `hasher graph.EventHasher` field; remove `truncateDiff`,
  `parseDiffStat`, `maxDiffBytes`.
- modify: `cmd/capture.go` — wire `infraGraph.NewSHA256Hasher()` into
  `NewCaptureService`; build the `EventRecord` via the task-005 `buildEventRecord`
  path before calling `Capture`.

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

Make task-006 GREEN. Prose control flow + the constructor signature change only —
NO bodies.

### application/capture_service.go

New constructor signature (adds the hasher port; keeps `git` only for the cold
Enrichment/Reconciliation paths owned by later tasks — the hot path must not call
it):

```go
type CaptureService struct {
	repo   graph.GraphRepository
	git    graph.GraphGitClient
	idGen  graph.SessionIDGenerator
	hasher graph.EventHasher
}

func NewCaptureService(
	repo graph.GraphRepository,
	git graph.GraphGitClient,
	idGen graph.SessionIDGenerator,
	hasher graph.EventHasher,
) *CaptureService
```

New `Capture` control flow (best-practices.md §2.1 — single tiny INSERT,
sub-ms append, silent skip on contention; architecture.md System Overview HOT
PATH 1–3):

1. Handle `EndSession` as today (no event appended).
2. Build/receive the `EventRecord` (source/kind/tool/instance/payload_raw already
   redacted upstream by task-005); set `RecordedAt`, `EventID` via `idGen`.
3. `prev, err := repo.HeadHash(ctx)` — current chain head.
4. `e.PrevHash = prev`; `e.ThisHash = hasher.Hash(prev, e)`.
5. `persisted, err := repo.AppendEvent(ctx, e)` — the only writer into `events`;
   one `BEGIN IMMEDIATE` INSERT (the atomicity lives in task-003's repository impl).
6. On `SQLITE_BUSY` (lock contention): return a non-error skip result (or nil) so
   the cmd layer exits 0 with a stderr warning — capture never blocks the agent
   (FR13, best-practices.md §2.1 "silent skip on contention"). Distinguish
   busy-skip from genuine errors.
7. On success: return a `CaptureResult` (seq/event_id/source) — no
   `FilesChanged`/diff work on the hot path.

Remove `truncateDiff`, `parseDiffStat`, `maxDiffBytes`, and all
`DiffNameOnly`/`HashObject`/`GetCaptureBaseline`/`DiffForFiles`/`CreateActionBatch`
calls from `Capture`. `sessionTimeoutMins` stays (used by projection/session logic).

### cmd/capture.go

In `captureOnce`, construct the hasher and pass it through:

```go
captureSvc := application.NewCaptureService(
	repo, graphGit,
	infraGraph.NewUUIDSessionIDGenerator(),
	infraGraph.NewSHA256Hasher(),
)
```

Build the `EventRecord` from the piped payload via task-005's `buildEventRecord`
(redaction applied) before invoking `Capture`; on `ok == false` (interactive/
malformed) fall back to the existing flag-only behavior and never error (FR13).

## Steps
1. Re-read architecture.md System Overview (HOT PATH) + "application/" and
   best-practices.md §2 (§2.1 sub-ms append, silent skip).
2. Change `CaptureService` struct + `NewCaptureService` to add the hasher field.
3. Rewrite `Capture` to the append-only flow above; delete the diff/baseline
   helpers and calls.
4. Wire `NewSHA256Hasher()` (task-003/infra) and `buildEventRecord` (task-005)
   in `cmd/capture.go`.
5. Run gofmt.

## Verification
- `go test ./application/...` — task-006 GREEN.
- `go build ./...` — succeeds.
- `gofmt -l application/capture_service.go cmd/capture.go` — prints nothing.
