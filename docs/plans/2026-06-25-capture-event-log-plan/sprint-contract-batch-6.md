# Batch 6 Sprint Contract

## Tasks

| ID | Subject | Type |
|----|---------|------|
| 020 | Scope-boundary regression (code index unaffected) | test |

## Acceptance Criteria

### Task 020: Scope-boundary regression — verification gate

- [ ] The existing code-graph index tests (impact, index, AST, co-change) pass unchanged: `go test ./... -count=1` is fully green.
- [ ] The code index is NOT modified by the redesign: `git diff feat/code-graph-sqlite...feat/capture-event-log` touches NONE of the index source — the `commits`/`files`/`authors`/`modifies`/`authored`/`co_changed`/`renames` tables/DDL, the AST layer (`ast_*`, FTS5), the indexer, or `Impact`/`ResolveRenames` query logic — except for additive, read-only reuse (provenance/diagnose calling `Impact`/`ResolveRenames`) and the shared `GraphRepository` interface gaining capture-only methods.
- [ ] Confirm the diff scope is limited to: the capture subsystem (event log, projections, reconcile, verify, provenance, diagnose, capture/payload/redaction), the `graph`/`diagnose` commands, capture-only schema (`events`/`event_files`, projection tables), and the design+plan docs.
- [ ] Repo-wide marker grep clean: `grep -rn -E '(TODO|FIXME|HACK|XXX|STUB|stub\b)' application cmd domain infrastructure pkg e2e` returns no matches.
- [ ] Full suite: `go build ./...`, `go vet ./...`, `gofmt -l .` clean, `go test ./... -count=1` green (incl. e2e).

## Red-Green Pairs

None — this is a run-only regression/verification task.

## Evaluation Criteria Preview

| Item ID | Description |
|---------|-------------|
| CODE-VER-01 | All verification commands exit with code 0 |
| CODE-QUAL-01 | No TODO/FIXME/HACK/XXX/STUB/stub markers in produced files |
| CODE-QUAL-02 | No stub implementations |

## Sign-off

- **Generator:** executing-plans
- **Status:** READY
- **Revision:** 0
