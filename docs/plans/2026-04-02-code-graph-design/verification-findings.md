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

## Predictive accuracy — temporal hold-out backtest

Does `impact` actually predict the files a real change needs? Measured by
leak-free backtest on this repo's own history (272 commits): for each feature
commit C, roll back to C~1, index only prior history, seed `impact` with one
changed file (the established file with the most prior history), and check how
many of C's OTHER changed files it predicts in the top 10. Baseline: predict the
globally most-frequently-changed files (popularity).

| metric (79 real feature commits, top-10)        | git-agent impact | popularity |
|-------------------------------------------------|------------------|------------|
| mean recall of held-out files                   | **46%**          | 32%        |
| commits with ≥1 correct prediction              | **66%**          | 51%        |

Multi-seed aggregation, same held-out set, 1 vs 2 seeds (stricter subset of 12
commits with ≥3 files): mean recall **18% with two seeds vs 9% with one** — e.g.
seeds `[cmd/commit.go, application/commit_service.go]` recover
`application/commit_service_test.go` that a single seed misses. 38% of the files
the two-seed query recovered were coupled to BOTH seeds (the aggregation signal).

Honest limitation, shown concretely: for `feat(domain): add scope whitelist`,
seeding from `validator.go` correctly flagged `validator_test.go` (80%) but
missed `infrastructure/hook/composite_executor.go` — because that pair had NEVER
co-changed before this commit. Co-change predicts from past coupling; a
first-ever coupling is unpredictable by construction. The framework is a strong
assist (catches ~half the related files from one seed, ~2× a naive guess, more
with multiple seeds), not an oracle.

## External validation — a third-party repo (cli/cli, 11,450 commits)

Repeated the temporal hold-out backtest on `github.com/cli/cli` — a large,
actively-developed product the framework was never tuned for.

| metric (39 real feature commits, top-10)        | git-agent impact | popularity |
|-------------------------------------------------|------------------|------------|
| mean recall of held-out files                   | **57%**          | 5%         |
| commits with ≥1 correct prediction              | **69%**          | 10%        |

The baseline collapses on a large modular codebase (popularity means little when
no file dominates), so co-change wins by ~11×. Textbook hits: `clone.go` →
`clone_test.go`, `view.go` → `view_test.go`, `feature_detection_test.go` →
`detector_mock.go`, the three `third-party-licenses.{darwin,linux,windows}.md`
predicting each other.

### Performance fixes surfaced by the large repo

First-run indexing of 11,450 commits dropped from **11.4s → 2.3s (≈5×)**:
batching all per-commit writes into one transaction (3.2s → 0.34s) and running
`ANALYZE` before the co-change self-join, without which the pure-Go SQLite
planner chose a nested-loop scan (recompute 6.4s → 0.22s). Results are byte-for-
byte identical; only speed changed. Robustness held up: reindex is idempotent,
incremental indexing of a new commit is ~96ms, and a directory seed that expands
to 100+ files now prints a bounded summary instead of a wall of paths.

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
