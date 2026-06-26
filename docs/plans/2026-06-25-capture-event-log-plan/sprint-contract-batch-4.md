# Batch 4 Sprint Contract

## Tasks

| ID | Subject | Type |
|----|---------|------|
| 012 | Projections & rebuild (test) | test |
| 013 | Projections & rebuild (impl) | impl |
| 014 | Out-of-band reconciliation (test) | test |
| 015 | Out-of-band reconciliation (impl) | impl |

## Acceptance Criteria

### Task 012: Projections & rebuild (test) — Red

- [ ] Test file compiles; tests FAIL because `ProjectionRebuilder` is absent.
- [ ] Rebuild replays the Event Log into `sessions`/`actions`/`action_modifies`/`event_files`; two rebuilds produce byte-identical projections (deterministic).
- [ ] Two interleaved `instance_id`s -> two sessions on one unforked chain.
- [ ] Rebuild runs `VerifyChain` first and refuses on a `ChainBreak`.
- [ ] Uses a real temp SQLite db.

### Task 013: Projections & rebuild (impl) — Green

- [ ] `application/projection_service.go` `ProjectionRebuilder.Rebuild`: verify-first; reset derived tables; `StreamEvents` replay -> sessions (group by `source`,`instance_id` with `sessionTimeoutMins` gap), actions (per Event; per-session running `sequence`), `action_modifies`/`event_files` (file paths from `tool_input`; File Blob Refs via cold-path `HashObject`). Determinism: ordering/timestamps derived solely from Event fields + chain order.
- [ ] `cmd/graph_rebuild.go` attaches to the existing `graph` parent command (`cmd/graph.go`).
- [ ] **Restore timeline e2e coverage** (Batch-2 carry-forward): add e2e that runs `graph rebuild` then asserts `timeline`/sessions reflect the captured Events. Update task-020's run-only e2e note as needed.
- [ ] All task-012 tests pass; build + gofmt clean; `go test ./... -count=1` no regressions.

### Task 014: Out-of-band reconciliation (test) — Red

- [ ] Test file compiles; tests FAIL because `ReconcileService` is absent.
- [ ] A working-tree change with no corresponding Event yields an Out-of-Band Event (`source = unknown`, `kind = out-of-band`) appended to the same chain, carrying `before_blob`/`after_blob` File Blob Refs.
- [ ] Reconciliation covers ONLY the unexplained residual (files already covered by recent Events are skipped).
- [ ] Fake `GraphGitClient` (`DiffNameOnly`/`HashObject`) + temp db.

### Task 015: Out-of-band reconciliation (impl) — Green

- [ ] `application/reconcile_service.go` reuses `GraphGitClient.DiffNameOnly`/`HashObject` (the calls removed from the hot path); compares working-tree hashes vs the state implied by replaying Events to HEAD; appends `source=unknown`/`kind=out-of-band` Events to the same chain (tamper-evident). Hook points per architecture.md "Reconciliation".
- [ ] All task-014 tests pass; build + gofmt clean; `go test ./... -count=1` no regressions.

## Red-Green Pairs

| Test Task | Impl Task | Expected Red State | Expected Green State |
|-----------|-----------|--------------------|----------------------|
| 012 | 013 | Tests fail (no ProjectionRebuilder) | Pass after task 013 |
| 014 | 015 | Tests fail (no ReconcileService) | Pass after task 015 |

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
