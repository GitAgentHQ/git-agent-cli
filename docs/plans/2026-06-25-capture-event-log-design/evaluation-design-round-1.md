# Design Evaluation — Round 1

**Design folder:** `docs/plans/2026-06-25-capture-event-log-design/`
**Checklist:** `docs/retros/checklists/design-v1.md`
**Mode:** design

## Checklist Results

| Item ID | Check | Result | Evidence |
|---|---|---|---|
| JUST-01 | Design must not self-declare NOT-JUSTIFIED | PASS | No not-justified marker; status line is "design (pre-merge redesign…)". |
| REQ-TRACE-01 | Every `REQ-NNN` ID appears in a scenario | PASS | Design uses `FR`/`NFR` IDs; all 15 FRs / 6 NFRs are traced into scenarios (FR4→verify, FR5→outcome, FR9→reconciliation). |
| SCEN-CONC-01 | All Given clauses use specific data values | FAIL → fixed | `bdd-specs.md:81` and `:158` contained the term "valid" (computational `\bvalid\b` match). Reworded; see below. |
| ARCH-01 | No inner-to-outer layer dependencies described | PASS | `EventHasher` is a domain port implemented in infra; dependencies point inward. |
| RISK-02 | Each risk mitigation specifies a concrete action | PASS | Vacuous (no Risks section); risks/trade-offs are stated in `_index.md` Rationale and `best-practices.md` with concrete mitigations. |

## Rework Applied

- `bdd-specs.md:81` — "whose self-hashes are individually valid" → "whose self-hashes each recompute to their stored this_hash".
- `bdd-specs.md:158` — "bytes that are not valid JSON" → "the bytes `{not json` that fail JSON parse".

After rework, `grep -nE "\bvalid\b" bdd-specs.md` returns zero matches in `Given` clauses.

## Verdict

**REWORK** (Round 1) — single computational FAIL on `SCEN-CONC-01`, a checklist-precision
issue (the word "valid" used in a concrete technical sense), resolved by the two
rewordings above. All other items PASS. Code premises verified accurate against
`feat/code-graph-sqlite` (`claudeHookPayload` parses only tool_name/session_id;
`CaptureService.Capture` is diff-reconstruction with a net-zero `skipped` path).

Round 2 expected to PASS post-rework.
