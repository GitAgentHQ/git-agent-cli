# Code Graph — Verification Findings

End-to-end validation of the existing code-graph capabilities on real and
synthetic git repositories (all experiments run under `/tmp`, never against
this repo). Drives the fixes committed alongside this document.

## Structural awareness — `impact` (P0)

Verified on a real 259-commit clone of this repository. Co-change ranking is
accurate and useful: querying `application/commit_service.go` surfaces, in
order, its test file (56%), `cmd/commit.go` (32%), and the openai/git client
collaborators (18%) — exactly what an agent asking "what else changes with
this file" needs. Transitive `--depth` correctly widens the frontier
(depth 2 → second hop, depth 3 → third).

Bugs found and fixed:

- **Path normalization** — `./path`, absolute paths, and subdirectory-relative
  paths returned nothing because the graph stores repo-relative paths and the
  CLI passed the raw argument through. Now normalized via git pathspec
  semantics with symlink resolution (macOS `/tmp` → `/private/tmp`).
- **`--min-count` honesty** — the index hard-pruned pairs below count 3, so
  `--min-count 2` silently returned nothing. Index floor lowered to 2;
  query default stays 3.
- **Transitive depth labeling** — indirect (depth > 1) results are now marked
  `[indirect, depth N]` so a second-hop coupling is not misread as direct.

## Behavioral traceability — capture → timeline (P1b)

The "edit → automatic capture → timeline shows the real file" loop now runs
end to end:

- **R21 auto-capture** — `git-agent init --agent-hook` installs a Claude Code
  `PostToolUse` hook (matcher `Edit|Write|Bash`) into `.claude/settings.json`,
  merge-safe and idempotent. `capture` reads `tool_name` and `session_id` from
  the hook's stdin payload, so Claude Code sessions map to capture sessions.
  Measured latency 52–60 ms internal (budget < 200 ms), no LLM.
- **Self-pollution fix** — capture excluded its own `.git-agent/` DB files
  (and now `.claude/`); previously the timeline headline was
  `Edit .git-agent/graph.db` instead of the real file.
- **Line counts (R23)** — `action_modifies.additions/deletions` were hardcoded
  to 0; now parsed from the action diff (verified: a 4-add/1-delete edit is
  recorded as +4/-1).

## Feature-level impact — multi-seed aggregation

A feature is a SET of files, not one. `impact` now accepts several seeds and
aggregates their co-change neighbours so a file coupled to many of the feature's
files ranks above one coupled to a single file — the files most likely to also
need changes surface first. Three ways an agent supplies the feature:

- **Multiple files** — `impact a.go b.go` ranks shared neighbours first, each
  annotated `[N/M seeds: ...]` (and `related_to` in JSON).
- **A directory** — `impact internal/auth/` expands to the tracked files under it.
- **No arguments** — seeds are the current working-tree changes: "given what
  I've already edited, what else usually moves with it?" — the natural call for
  an agent mid-task. Tooling dirs (`.git-agent/`, `.claude/`) are never seeds.

Each result carries `score` (sum of coupling strengths over matched seeds),
`seed_matches`, and `related_to`, so an agent can prioritise which related
files to open. Verified on the real 259-commit clone: querying
`commit_service.go` + `cmd/commit.go` correctly surfaces `commit_service_test.go`
and the openai/git/config collaborators as `[2/2 seeds]`.

## Structural awareness in the commit flow — co-change A/B

Question: does injecting co-change hints into the commit planner improve
grouping? Method: a synthetic repo with files `w,x,y,z` whose names give no
grouping signal, history coupling `w↔x` and `y↔z` (≈11 each, no cross-edges),
then a multi-file working change planned `--dry-run` with and without the
graph DB present. Model: `gemini-3.1-flash-lite` via proxy.

| Arm | Correct grouping ({w,x}+{y,z}) |
|-----|-------------------------------|
| WITH graph (hints injected)    | 3 / 4 runs |
| WITHOUT graph                  | 2 / 4 runs |

Findings, recorded honestly:

- Hints **are** injected (verbose logs `found 2 co-change hints for planning`).
- When the planner splits a multi-file change, it generally respects co-change
  pairing — a directionally positive but weak effect at this sample size.
- The hint is a **soft** signal. For small, cohesive diffs the model collapses
  everything into one commit regardless, and an adversarial test (file names
  suggesting the opposite of history) did not flip grouping because the model
  declined to split at all.
- Prompt wording was strengthened from "consider grouping" to "Keep each pair
  in the SAME commit group unless their diffs are clearly unrelated" — more
  directive, still conditional, no test regression.

Conclusion: the enhancement functions as designed; its observable benefit is
real but model- and scenario-dependent, strongest when the change naturally
splits into multiple commits. Not a forcing constraint, and intentionally so.
