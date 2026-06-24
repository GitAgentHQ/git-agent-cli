# Code Graph — Verification Findings

End-to-end validation of the existing code-graph capabilities on real and
synthetic git repositories (all experiments run under `/tmp`, never against
this repo). Drives the fixes committed alongside this document.

> **READ THIS FIRST — the headline numbers answer the wrong question.**
> The recall figures below (46–57%) compare `impact` against a *popularity*
> baseline (the globally most-changed files). That is a strawman. The decision
> that matters is whether `impact` helps an LLM agent **beyond what the agent
> finds by reading the code itself**. Measured against that baseline (next
> section), the unique marginal value collapses to ~0.2–0.4 non-obvious files
> per commit, almost all of them recurring CI/build-config pairs — close to
> noise for an agent that can read the repo. Weigh everything below against that.

## Marginal value to a code-reading agent — the decisive eval

The earlier backtests proved `impact` is a *better co-change predictor than
popularity* and is language-agnostic. They did **not** test the question that
decides whether the feature earns its complexity: does it surface files an LLM
agent (large context, can grep/read) would otherwise miss?

Re-ran the temporal hold-out with a **generous agent proxy** as the baseline:
for each held-out file, mark it "obvious" if an agent would find it by
(a) the impl↔test naming pair, (b) being in the seed's directory, or
(c) a textual cross-reference between the two files. Only count a prediction as
*valuable* if it is correct **and** non-obvious. Also bucket each held file by
prior co-occurrence with the seed (0 = novel, unpredictable by construction).

| metric | flask (Py) | express (JS) | git-agent (Go) |
|--------|-----------:|-------------:|---------------:|
| raw recall (sanity)                          | 38% | 48% | 29% |
| correct hits that are trivial (test/dir/xref) | **82%** | 53% | **83%** |
| correct hits that are non-obvious            | 18% | 47%* | 17% |
| held files never co-changed before (novel)   | 26% | 11% | 29% |
| held files novel **or** sub-threshold        | 43% | 29% | **50%** |
| misses that were unpredictable (novel/weak)  | 69% | 54% | 70% |
| **non-obvious correct predictions / commit** | 0.33 | 0.37 | **0.18** |

Findings, against the user's skepticism — all confirmed:

- **Most hits are trivial.** 82–83% of correct predictions (flask, git-agent)
  are files an agent finds for free. Only ~17% are non-obvious.
- **The new-coupling blind spot is real and large.** 43–50% of held files never
  (or barely) co-changed before, so `impact` cannot predict them in principle;
  54–70% of all misses are these unpredictable files. On the most active repo
  (git-agent, real feature work) half the held files are novel — the tool is
  weakest exactly when doing new work.
- **Marginal value over a reading agent is ~0.2–0.4 files/commit, and that is an
  upper bound.** The cross-reference proxy only matches file *names*, so it
  misses interface-based dependencies (e.g. git-agent's `commit_service.go` →
  `git/client.go`, found via the `GitClient` type, not the string "client") —
  an agent reading the file would still find those, so the true unique value is
  lower than measured.
- **\*The non-obvious wins are a few recurring config/tooling pairs, not code.**
  De-duplicated, they are: `appveyor.yml → ci.yml` (express, the single pattern
  behind its 47%), `requirements/dev.txt → .pre-commit-config.yaml`,
  `pyproject.toml → requirements/*`, `tox.ini → ci.yaml` (flask). Genuine
  "changing A surprisingly needs unrelated code Z" hits were ~zero in the sample.

Independent corroborating signal: the commit-grouping A/B (below) already showed
co-change hints have only a soft, marginal effect when fed to the LLM planner.

**Verdict.** For an LLM agent that can read the codebase, `impact`'s unique
contribution is near noise, concentrated in a handful of cross-tooling config
couplings. The SQLite graph + indexing + capture-hook complexity is hard to
justify on this evidence. Reasonable options: (a) drop the graph subsystem;
(b) keep a zero-dependency `impact` that computes co-change directly from
`git log` with no SQLite/index/hooks, preserving the thin config-pair value
without the weight; (c) if the real audience is *humans* navigating an
unfamiliar large repo (where evolutionary coupling has established value), the
positioning and the evaluation method both need to change — test with humans,
not a proxy.

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

## External validation — third-party repos, two languages

Repeated the same leak-free temporal hold-out on real projects the framework was
never tuned for, including a non-Go codebase to confirm it is language-agnostic
(co-change is computed from git history alone — no parsing, no language rules).

| repo (lang)              | commits | recall (impact) | recall (baseline) | hit-rate (impact) |
|--------------------------|--------:|----------------:|------------------:|------------------:|
| git-agent (Go)           |     272 |          **46%** |              32% |              66% |
| cli/cli (Go)             |  11,450 |          **57%** |               5% |              69% |
| expressjs/express (JS)   |   6,153 |          **47%** |               0% |              55% |
| pallets/flask (Python)   |   5,539 |          **51%** |               2% |              69% |

Consistently ~46–57% recall, always far above the popularity baseline — which
collapses to near zero on larger, modular codebases (no single file dominates,
so "most-changed" predicts nothing). Textbook hits across languages: `clone.go`
→ `clone_test.go`, `view.go` → `view_test.go`, `lib/response.js` →
`test/res.redirect.js`, and even cross-tooling signals like
`appveyor.yml` → `.github/workflows/ci.yml` (a CI migration) that no
language-specific tool would catch. High-fanout noise (changelogs, package.json)
is demoted by coupling strength without any language-specific filtering — e.g.
Express's `History.md` co-changes 42× but ranks low at strength 0.04.

### Performance fixes surfaced by the large repo

First-run indexing of 11,450 commits dropped from **11.4s → 2.3s (≈5×)**:
batching all per-commit writes into one transaction (3.2s → 0.34s) and running
`ANALYZE` before the co-change self-join, without which the pure-Go SQLite
planner chose a nested-loop scan (recompute 6.4s → 0.22s). Results are byte-for-
byte identical; only speed changed. Robustness held up: reindex is idempotent,
incremental indexing of a new commit is ~96ms, and a directory seed that expands
to 100+ files now prints a bounded summary instead of a wall of paths.

## Positive result — recency weighting (shipped)

Weighted each co-change by an exponential decay of its commit age (one-year
half-life), so recent couplings dominate strength and stale ones fade; the raw
count still drives the min-count floor. Validated with a *paired* backtest — the
same held-out commit scored by both the old (symmetric, all-time) and new
(recency) ranking on an identical test set:

| repo (maturity)          | symmetric | recency | per-commit recency better / worse |
|--------------------------|----------:|--------:|-----------------------------------|
| flask (Python, ~14 yr)   |     50.7% | **56.4%** | 19 / 1 |
| express (JS, ~14 yr)     |     46.9% | **56.3%** | 12 / 1 |
| git-agent (Go, ~1 yr)    |     47.7% |   47.7% | 2 / 1 |

Recency lifts recall 6–9 points on mature, long-lived repos (their old coupling
patterns are stale) and is neutral on a young repo — and never meaningfully
regresses a commit. Shipped. Contemporaneous history (e.g. unit-test fixtures
committed at once) decays to plain count-based strength, so existing behaviour
is unchanged where there is no time spread.

## Negative result — directional coupling does not help

Tested ranking neighbours by directional `P(neighbour | seed) = count / seed's
own change count` instead of the stored symmetric strength `count / max(totalA,
totalB)`. Theory favours directional for prediction, but the backtest on express
showed **no recall gain (46% vs 47%)** while it **promoted high-fanout noise** —
the `History.md` changelog jumped to rank 1 for a core file, because
`P(changelog | anything)` is moderate. The symmetric denominator demotes files
that change with everything, which matters more than the marginal theory gain.
Kept symmetric; reverted directional.

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
