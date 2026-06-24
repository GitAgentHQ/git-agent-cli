# Task 020: Scope-boundary regression (code index unaffected)

**type**: test
**depends-on**: ["007", "009", "011", "013", "015", "017", "019"]

## Files
- modify (run-only, no edits to test bodies): `e2e/impact_ast_test.go`,
  `e2e/capture_timeline_test.go` and the existing `application`/`infrastructure`
  index/impact unit tests — execute them unchanged and confirm identical results.
- create (only if a guard is missing): a small regression guard test asserting
  the untouched index files were not modified by the redesign (a source/diff
  check), placed beside the existing index tests.

## BDD Scenario(s)
```gherkin
  Scenario: The code index is unaffected by the capture redesign
    Given an indexed repository with commit, file, AST, and co-change data
    When the capture subsystem is replaced by the Event Log
    Then existing impact and index queries return the same results
```

## What to implement

A regression task — no new product behavior. Confirm the code-graph index
(commits/files/AST/co-change/impact) is untouched by the capture redesign, per
architecture.md "Integration Points — Untouched" and the hard scope boundary in
`_index.md` ("Scope boundary"). best-practices.md §3 "Scope-boundary regression:
existing impact/index e2e tests must pass unchanged."

Two checks:

### A. Existing index/impact tests pass unchanged

Run the EXISTING impact/index suites without modifying their assertions and
confirm identical results to the pre-redesign branch:

- `e2e/impact_ast_test.go` — AST-based structural impact (`impact --symbol`,
  cross-file call resolution).
- The existing co-change `Impact` unit tests under `application/` and the SQLite
  `Impact`/`ResolveRenames`/co-change tests under `infrastructure/`.

These must be byte-for-byte the same test bodies as on `feat/code-graph-sqlite`;
the task only verifies they still pass after tasks 007/009/011/013/015/017/019.

### B. Untouched-files guard

Assert the redesign did not modify the index layer named "Untouched" in
architecture.md "Integration Points":

- Tables: `commits` / `files` / `authors` / `modifies` / `authored` /
  `co_changed` / `renames` (`sqlite_client.go:334-389`) — their DDL is unchanged.
- Methods: `UpsertCommit` / `CreateModifies` / `RecomputeCoChanged` /
  `IncrementalCoChanged` / `Impact` / `ResolveRenames` — signatures and behavior
  unchanged.
- The entire AST layer + FTS5 and `indexer.go` — unchanged.

Implement as a diff/source check: compare the index-owning regions/files between
`feat/code-graph-sqlite` and the redesign HEAD (via `git show
feat/code-graph-sqlite:<path>` vs working tree) and assert the index DDL blocks
and the AST/`indexer.go` files are not modified by the redesign. (The events /
event_files DDL is *additive*; the `capture_baseline` DDL removal and the
sessions/actions/action_modifies/action_produces demotion to Projections are the
*only* expected schema deltas — those are capture, not index.)

## Steps
1. Read architecture.md "Integration Points" (Changes vs Untouched) and
   `_index.md` "Scope boundary".
2. Run the existing index/impact e2e + unit tests unchanged; capture results.
3. Diff the index DDL blocks (`commits`/`files`/`authors`/`modifies`/`authored`/
   `co_changed`/`renames`), the AST layer, FTS5, and `indexer.go` against
   `feat/code-graph-sqlite`; assert no redesign edits to those regions.
4. If no existing guard covers (B), add the minimal source/diff guard test beside
   the index tests; otherwise rely on the diff check in step 3.

## Verification
- `go test ./e2e/... ./application/... ./infrastructure/...` — the existing
  index/impact tests (`TestImpactAST_*`, the co-change `Impact` unit tests, the
  SQLite `Impact`/`ResolveRenames` tests) pass unchanged.
- `git show feat/code-graph-sqlite:infrastructure/graph/sqlite_client.go` vs the
  working tree — the `commits`/`files`/`authors`/`modifies`/`authored`/
  `co_changed`/`renames` DDL (lines 334-389) and the AST/FTS5 DDL are byte-for-byte
  unchanged by the redesign; only capture DDL (`events`/`event_files` added,
  `capture_baseline` removed) differs.
- `git diff feat/code-graph-sqlite -- <ast files> infrastructure/graph/indexer.go`
  — empty (index/AST layer untouched).
