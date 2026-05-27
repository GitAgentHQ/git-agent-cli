# Sprint Contract — Batch 3 (RECOVERY, supersedes v1)

**Revision:** 2 (recovery after coordinator stall)
**Original contract archived at:** `sprint-contract-batch-3.md` is the v1 (still on disk; do NOT delete)
**Batch composition:** identical to v1 — 3 Red-Green pairs (6 tasks total)
**Plan:** `docs/plans/2026-05-27-large-diff-stuck-plan/`
**Code checklist:** `docs/retros/checklists/code-v1.md`

## Why this revision exists

The Batch 3 coordinator (agent ab94...) stalled mid-execution after ~13 minutes ("no progress for 600s") while working on pair P6 (openai token ceiling) impl. Last reported intent: "Now update the four call sites." Working tree state inspection by the main agent confirmed:

- P6 (token ceiling): ~90% complete — constants declared, callLLM signature changed, ceiling branch inserted, Generate + Plan call sites updated. **Two call sites still need updating** (DetectTechnologies at line 551, GenerateScopes at line 581 — both currently passing only 4 args to a 5-arg function, causing a compile error).
- P6 test (`TestClient_TokenCeiling`): not written.
- P9 (synopsis fallback): not started.
- P10 (heuristic fallback): not started.

The build is currently broken (vet error at line 551). Recovery coordinator picks up from this state without re-doing P6's already-completed work.

## Tasks in this batch (unchanged from v1)

| TaskList ID | Task file | Role | State |
|---|---|---|---|
| #11 | task-006-openai-token-ceiling-test.md | RED | NOT STARTED |
| #12 | task-006-openai-token-ceiling-impl.md | GREEN | ~90% complete (2 call sites + verify left) |
| #17 | task-009-app-synopsis-fallback-test.md | RED | NOT STARTED |
| #18 | task-009-app-synopsis-fallback-impl.md | GREEN | NOT STARTED |
| #19 | task-010-app-heuristic-fallback-test.md | RED | NOT STARTED |
| #20 | task-010-app-heuristic-fallback-impl.md | GREEN | NOT STARTED |

## Acceptance criteria

All criteria from `sprint-contract-batch-3.md` v1 carry over verbatim. The only difference is that some P6-impl criteria are already satisfied; the recovery coordinator confirms the existing state rather than re-implementing.

**Verification of P6-impl pre-existing state (treat as a confirmation gate before continuing):**

- `infrastructure/openai/client.go` lines 36-39 declare the four ceiling constants (`planMaxTokensCeiling=16384`, `generateMaxTokensCeiling=16384`, `scopesMaxTokensCeiling=16384`, `detectMaxTokensCeiling=4096`).
- `callLLM` signature at line 261 is `(ctx, system, user string, maxTokens, maxTokensCeiling int) (string, error)`.
- Lines 316-326 contain the ceiling-aware FinishReasonLength branch returning `&commit.PlannerBudgetExhaustedError{Model: c.model, Ceiling: maxTokensCeiling}`.
- Lines 453, 510 already pass per-endpoint ceilings.
- **TODO(batch-3) marker is already removed** (no further action needed).

**Remaining P6-impl work for the recovery coordinator:**

- Update line 551 (`DetectTechnologies`): pass `detectMaxTokensCeiling` as the 5th arg.
- Update line 581 (`GenerateScopes`): pass `scopesMaxTokensCeiling` as the 5th arg.
- Run `go vet ./infrastructure/openai/...` to confirm the build is no longer broken.

**Then write P6-test, and execute P9, P10 per their original criteria.**

All other acceptance criteria (P9-test, P9-impl, P10-test, P10-impl, cross-cutting) carry over verbatim from `sprint-contract-batch-3.md` v1.

## Sign-off

- **Acceptance criteria authoring:** auto-derived from task files; do NOT add new criteria.
- **Coordinator verdict:** PASS only if every acceptance criterion in v1 is satisfied AND `grep -r "TODO(batch-3)" . --include='*.go'` returns zero matches.
- **Rework budget:** maximum 2 evaluator-rework rounds before escalation.
- **Revision history:**
  - v1 (initial) — coordinator stalled after ~13 min
  - v2 (this file) — recovery, picks up from partial P6 state
