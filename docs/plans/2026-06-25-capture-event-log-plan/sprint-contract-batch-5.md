# Batch 5 Sprint Contract

## Tasks

| ID | Subject | Type |
|----|---------|------|
| 016 | graph provenance (test) | test |
| 017 | graph provenance (impl) | impl |
| 018 | graph diagnose (test) | test |
| 019 | graph diagnose (impl) | impl |

## Acceptance Criteria

### Task 016: graph provenance (test) — Red

- [ ] Test file compiles; tests FAIL because the provenance service/command is absent.
- [ ] `graph provenance <file>` returns a chronological, rename-aware (`ResolveRenames`) history merging Event-log changes (via `event_files`) + Out-of-Band Events.
- [ ] Out-of-Band rows are flagged with `source = unknown`.
- [ ] Uses a real temp SQLite db.

### Task 017: graph provenance (impl) — Green

- [ ] `application/provenance_service.go` + `cmd/graph_provenance.go` (attach to the `graph` parent). Reuses `ResolveRenames` + `event_files` join; merges Out-of-Band Events.
- [ ] All task-016 tests pass; build + gofmt clean.

### Task 018: graph diagnose (test) — Red

- [ ] Test file compiles; tests FAIL because the diagnose service is absent.
- [ ] Diagnose ranks the breaking action within the Suspect Window (last-green -> first-red Outcome Events); highest-ranked Candidate is the most recent Event directly touching the seed file; each Candidate carries `before_blob`/`after_blob`.
- [ ] No green baseline -> result flagged `low_confidence: no_green_baseline`, window opens at genesis.
- [ ] Diagnose runs `VerifyChain` first; refuses (exit 4) on a `ChainBreak` unless `--force`.
- [ ] Optional LLM re-rank operates only over the deterministic top-N and cannot add Candidates (fake LLM in tests).

### Task 019: graph diagnose (impl) — Green

- [ ] `application/diagnose_service.go`: Suspect Window from Outcome Events; relevant-file set R expanded via `ImpactService.Impact` (read-only reuse); deterministic Candidate scoring (recency/impact_overlap/direct_seed_hit/churn/later_reverted with architecture.md weights); optional bounded LLM re-rank.
- [ ] Replace the `cmd/diagnose.go` stub with the real `graph diagnose`; verify-first (exit 4 via `pkg/errors.ErrChainIntegrity` unless `--force`).
- [ ] **Rework/rename `TestDiagnose_StubMessage`** (and any "stub message" assertions) to the real diagnose behavior — this ELIMINATES the last sanctioned CODE-QUAL-01 "stub" exemption. After this task, `grep -rn -E '(TODO|FIXME|HACK|XXX|STUB|stub\b)'` over produced + e2e files must be clean.
- [ ] All task-018 tests pass; build + gofmt clean; `go test ./... -count=1` no regressions.

## Red-Green Pairs

| Test Task | Impl Task | Expected Red State | Expected Green State |
|-----------|-----------|--------------------|----------------------|
| 016 | 017 | Tests fail (no provenance) | Pass after task 017 |
| 018 | 019 | Tests fail (no diagnose) | Pass after task 019 |

## Evaluation Criteria Preview

The evaluator will apply `docs/retros/checklists/code-v1.md`:

| Item ID | Description |
|---------|-------------|
| CODE-VER-01 | All verification commands exit with code 0 |
| CODE-QUAL-01 | No TODO/FIXME/HACK/XXX/STUB/stub markers in produced files |
| CODE-QUAL-02 | No stub implementations (NotImplementedError, pass-only, ellipsis-only bodies) |

Note: this batch should leave ZERO "stub" markers (task 019 reworks the diagnose stub). No exemption needed afterward.

## Sign-off

- **Generator:** executing-plans
- **Status:** READY
- **Revision:** 0
