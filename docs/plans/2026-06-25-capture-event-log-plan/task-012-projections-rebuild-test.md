# Task 012: Projections & rebuild (test)

**type**: test
**depends-on**: ["001"]

## Files
- create: `application/projection_service_test.go` — RED tests for `ProjectionRebuilder.Rebuild`
- create (if absent): a shared test helper for an on-disk SQLite `GraphRepository` over a temp `graph.db` (peer of existing infra tests)

## BDD Scenario(s)
```gherkin
Scenario: Projections rebuild deterministically from the Event Log
  Given a log of ten chained Events across two sessions
  And the sessions, actions, and action_modifies projections are dropped
  When "graph rebuild" runs
  Then the projections are reconstructed solely from the Event Log
  And a second rebuild produces byte-identical projections

Scenario: Concurrent agents are attributed to separate sessions from one chain
  Given two agents capture interleaved Events with different instance_id values
  When the Events are appended to the single shared chain
  And the session projection is built
  Then each instance_id maps to its own session
  And the chain remains a single unforked sequence

Scenario: Rebuild refuses on a broken chain
  Given an Event Log with a ChainBreak
  When "graph rebuild" runs
  Then it reports the break and refuses to rebuild projections
```

## What to implement

Failing tests for the cold-path Replay engine `ProjectionRebuilder.Rebuild(ctx)`
(architecture.md "Projections & Rebuild"). The engine reads the append-only
`events` table via `StreamEvents(ctx, 0)` in `seq` order and folds it into the
derived Projections (`sessions`, `actions`, `action_modifies`, `event_files`);
`action_produces` is left to the `LinkActionsToCommit` seam. Tests drive the real
`SQLiteRepository` over a temp `graph.db` so the assertions exercise the schema and
the fold together; git access is via a fake `GraphGitClient` (only `HashObject` is
exercised on the cold path, for File Blob Refs — architecture.md
"action_modifies / event_files").

Constructor/method under test (signatures only — NO bodies):

```go
// application/projection_service.go (created in Task 013)
type ProjectionRebuilder struct { /* repo graph.GraphRepository; git graph.GraphGitClient */ }

func NewProjectionRebuilder(repo graph.GraphRepository, git graph.GraphGitClient) *ProjectionRebuilder

func (r *ProjectionRebuilder) Rebuild(ctx context.Context) error
```

Tests to write (all must compile and FAIL because `projection_service.go` does not
yet exist / `Rebuild` is unimplemented):

1. `TestProjectionRebuilder_DeterministicRebuild` — append ten chained Events whose
   `instance_id` alternates between two values (two sessions), via `AppendEvent`.
   Drop / truncate the `sessions`, `actions`, `action_modifies`, `event_files`
   tables. Call `Rebuild` once; snapshot every Projection table (ordered dump of all
   columns). Call `Rebuild` a second time; snapshot again. Assert the two snapshots
   are **byte-identical** (architecture.md "Determinism: projections derive ordering
   and timestamps solely from Event fields + chain order"). Assert the rebuilt
   `sessions` count is 2 and the `actions` count is 10, and that the dump contains
   no rows sourced from anything but the Event Log.

2. `TestProjectionRebuilder_ConcurrentInstancesSplitSessions` — append Events whose
   `instance_id` values interleave (A, B, A, B, ...) on the **single shared chain**
   (one continuous `prev_hash`/`this_hash` sequence, no fork). After `Rebuild`,
   assert the session projection groups by `(source, instance_id)` so each
   `instance_id` maps to its own session row, and that `VerifyChain` still reports
   `ok` (the chain stayed a single unforked sequence). Use the
   `sessionTimeoutMins` gap rule (`capture_service.go:11`) — Events within the gap
   for one `instance_id` stay in one session.

3. `TestProjectionRebuilder_RefusesOnBrokenChain` — append three Events, then mutate
   `graph.db` directly to introduce a `ChainBreak` (e.g. edit `payload_raw` of the
   second Event without re-chaining). Call `Rebuild`; assert it returns a non-nil
   error that reports the break (carries the `ChainBreak` / its `seq`), and assert
   the Projection tables are **left untouched** (rebuild ran `VerifyChain` first and
   refused — architecture.md "Projections & Rebuild" step 1).

Reference: architecture.md "Projections & Rebuild", "Reconciliation", "Schema"
(`event_files`); best-practices.md §3 (Testing — determinism test, tamper tests).

## Steps
1. Add a temp-db helper that opens a real `SQLiteRepository` against a `t.TempDir()`
   `graph.db` and seeds N chained Events via `AppendEvent` with controllable
   `Source`, `InstanceID`, `Kind`, `ToolName`, `RecordedAt`, `PayloadRaw`.
2. Add a fake `graph.GraphGitClient` returning deterministic `HashObject` values.
3. Write the three tests above against `NewProjectionRebuilder(repo, gitFake)`.
4. Add a helper that dumps each Projection table to ordered bytes for the
   byte-identical comparison.

## Verification
- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/... -run TestProjectionRebuilder` — RED: tests compile and fail because `ProjectionRebuilder`/`Rebuild` is not yet implemented (failing for the right reason, not a build error in unrelated packages).
