# Testing History

A consolidated record of every testing pass the maintainer (Frad) has run on
git-agent-cli — the standing test suite plus the chronological end-to-end
validation rounds that shaped the graph feature. This documents *what was
tested, how, and what the results were*; it is not a test plan for future work.

For the current command matrix and how to run tests, jump to
[How to run tests](#how-to-run-tests). For the standing suite's current state,
see [The standing suite (current truth)](#the-standing-suite-current-truth).

---

## The standing suite (current truth)

git-agent-cli is BDD-driven TDD: a `.feature` spec is written first, then a RED
test, then GREEN code. The suite is pure Go, no cgo, no external services.

- **Unit tests** — `application/`, `domain/`, `infrastructure/`, `cmd/`, `pkg/`.
  Each `.feature` has a paired `_test.go`.
- **e2e tests** — `e2e/`. `TestMain` builds the `git-agent` binary once, then
  every test invokes it as a subprocess against a temp repo. Re-run
  `go test ./e2e/...` after any source change — the stale binary will not
  reflect edits.
- **BDD specs** — 18 `.feature` files across `application/`, `cmd/`,
  `domain/commit/`, `hooks/`, `infrastructure/diff/`, `infrastructure/graph/`,
  `infrastructure/openai/`.

### Most recent run — v0.7.0 release gate (2026-07-07)

Run inline during the `finish-release` workflow on `release/0.7.0` (HEAD
`6dd9375`, plus the CHANGELOG commit `75ca44a`). All green.

| Package | Result |
|---|---|
| `application` | ok (~6–9s) |
| `domain/commit` | ok |
| `domain/diff` | ok |
| `domain/graph` | ok |
| `infrastructure/config` | ok |
| `infrastructure/diff` | ok |
| `infrastructure/git` | ok |
| `infrastructure/gitignore` | ok |
| `infrastructure/graph` | ok |
| `infrastructure/hook` | ok (~14–16s, slowest) |
| `infrastructure/openai` | ok |
| `cmd` | ok |
| `pkg/filter`, `pkg/output` | ok |
| `e2e` | ok (~8.6s) |

Also: `go build .` clean; `gofmt -l .` clean; `go vet ./...` clean; secret
scan of the `main..develop` diff clean. This is the gate that let `v0.7.0`
ship.

### Earlier in-session run — PR #8 creation (2026-07-06)

Identical matrix, identical all-green result, run before opening
`develop → main` PR #8. Served as the pre-PR quality gate for the v0.7.0
cycle (co-change-only graph + command refactor + rename tracking + planner
file-list collapsing).

---

## Chronological E2E validation rounds

These are the held-out, real-repo validation passes that decided the graph
feature's direction. Each was run under `/tmp` against external repositories —
never against this repo's own history. They are recorded here because they are
not reproducible from the test suite alone; they were one-off experiments whose
*findings* drove commits.

### Round 1 — Code-graph E2E + marginal-value eval (2026-06-17)

Branch `feat/code-graph-sqlite`. Full write-up committed at
`docs/plans/2026-04-02-code-graph-design/verification-findings.md`.

**Method.** Drove the existing code-graph capabilities against real and
synthetic git repos. First a popularity-baseline backtest (recall vs the
globally most-changed files), then a **decisive re-test** against a
code-reading-agent proxy: for each held-out file, mark it "obvious" if an agent
would find it via (a) impl↔test naming, (b) same directory, or (c) a textual
cross-reference. Only count a prediction as *valuable* if correct **and**
non-obvious.

**Results (recall vs agent proxy):**

| metric | flask (Py) | express (JS) | git-agent (Go) |
|--------|-----------:|-------------:|---------------:|
| raw recall (sanity) | 38% | 48% | 29% |
| correct hits that are trivial | 82% | 53% | 83% |
| correct hits that are non-obvious | 18% | 47%* | 17% |
| held files novel or sub-threshold | 43% | 29% | 50% |
| **non-obvious correct predictions / commit** | 0.33 | 0.37 | 0.18 |

**Findings.**
- Most hits are trivial — 82–83% of correct predictions (flask, git-agent) are
  files an agent finds for free.
- The new-coupling blind spot is real and large — 43–50% of held files never
  (or barely) co-changed before; the tool is weakest exactly during new
  feature work.
- Marginal value over a reading agent is ~0.2–0.4 files/commit, an upper bound
  (the xref proxy under-credits interface-based deps).

**Also shipped this round:** multi-seed `impact` (FEATURE, not one file —
directory expansion, no-args uses working-tree changes, aggregate across
seeds); R21 auto-capture via `init --agent-hook` writing a Claude Code
`PostToolUse` hook (matcher `Edit|Write|Bash`, ~50–60ms latency); co-change
floor lowered 3→2; `action_modifies` add/delete parsed from diffs.

**Soft signal finding (co-change commit enhancement).** A/B on
`gemini-3.1-flash-lite` with vs without `graph.db`: hints ARE injected and the
planner tends to respect co-change pairing (3/4 vs 2/4 runs), BUT for small
cohesive diffs the model makes one commit regardless, and adversarial naming
did not flip grouping. Co-change is a **soft** signal — do not assume it
materially changes commit grouping.

### Round 2 — Graph A/B on cobra/yaml/cli (2026-06-27)

Re-ran on `main` (v0.5.1, binary `/tmp/git-agent-e2e`) per the question "Graph
vs no-Graph, with a model judging, on implementing a feature / solving an
issue." Three real Go repos: spf13/cobra v1.8.1, go-yaml/yaml v3.0.1,
urfave/cli v2.27.5. Full write-up: `/tmp/e2e/findings.md`.

**Method.** Two arms per task: A = no graph, B = with `.git-agent/graph.db`.
Arm identity verified via `commit --dry-run -v`'s `found N co-change hints`
line. Tasks = real feature/issue implementation on each repo.

**Results.**
- **Commit co-change lever is mechanically correct but soft.** B arms injected
  hints (cobra 1, yaml 3, cli 1); A arms injected 0. But grouping did not
  change in any run — model made 1 cohesive commit every time. Re-confirms
  Round 1's soft-signal caveat.
- **Forensic callers gave a real speed/thoroughness win (R1 cobra).**
  `callers Flag --depth 2` returned 3 cross-file receiver-inferred callers in
  `completions.go` that `grep Flag` buries; `callers mergePersistentFlags
  --depth 2` (48 edges) covered cross-file impact. Arm B patched BOTH
  `Flag()` and `mergePersistentFlags()` + tested all 4 lookup paths in ~4 min;
  arm A independently found the same root cause via grep in ~9 min and patched
  only `Flag()`. Both correct — a thoroughness/speed win for B, not unique
  correctness.
- **Weak model (qwen3.6-flash) gave up on all 3 tasks in both arms.** Graph
  cannot help a model that cannot attempt the task.

**Boundaries discovered (real, user-facing):**
- Graph does NOT index imported packages (cobra's flag handling lives in
  external `pflag`, so `Lookup`/`flag.go` are absent).
- Graph does NOT index struct fields (cli's `HideHelpCommand` is a `bool`
  field, so `callers HideHelpCommand` fails by design).
- **Symbol-collision bug (R2 yaml):** when two methods share a name across
  types (`parser.alias` vs `decoder.alias`), `graph callers alias`
  mis-resolves, and receiver-qualified forms ERROR "symbol not found". Filed
  as a defect; workaround was `grep \.alias(`.
- `graph index` does not populate co-change; `graph impact <file>` auto-indexes
  git history on first run (MinCount=3 hardwired in the commit-time provider).

**Recommendation recorded.** Keep graph; the forensic cross-method /
cross-package reach is the real value (the cobra win). Commit co-change lever
is correct but soft — don't market it as "grouping improvement". Don't expect
graph to rescue weak models.

### Round 3 — `related` grep-complementarity E2E (2026-07-01)

Validated the develop-branch binary (post-co-change-only cut) in `/tmp` against
real repos: gin (2004 commits), flask (5539 commits). This is the round that
validated the v0.7.0 `related` command after the AST subsystem was deleted.

**Method.** Build the co-change index, run `related <file>` queries, then
exclude the target commit (index at its parent) for a leak-free back-test.
Cross-check each top result against grep + symbol search to classify it as
grep-reachable or grep-blind.

**Results.**
- **Index build is fast** — gin 389ms, flask 996ms; queries ~0ms.
- **Leak-free back-test, strong coupling** — excluded the target commit,
  `related context.go` still predicted `context_test.go` at 56% / 126
  co-changes. Genuine prediction, no leakage.
- **Leak-free back-test, cross-cutting commit** — `related render/render.go`
  (PDF-renderer feature) surfaced the `render/` package family but MISSED
  `context.go` and `render/render_test.go`. Co-change is an aggregate signal:
  accurate for consistent impl↔test couplings, soft for feature-spanning
  changes.
- **grep-complementarity (the key positioning insight)** — of `context.go`'s
  top 12 co-change partners, 7 were grep-blind (no textual/symbol link to the
  seed: `tree.go`, `errors.go`, `binding/*`, `render/*`). In flask, `app.py`
  co-changes with `CHANGES.rst` (85×) and `docs/templating.rst` — pure
  grep-blind docs/changelog discipline. grep finds files by current
  content/symbol; `related` finds them by temporal co-change. They are
  complementary, not redundant.

**Recommended coding-agent workflow** (the output of all three rounds): (1)
`related <file>` for blast radius + the commits explaining WHY coupled (intent
context grep can't give); (2) grep/read those files for exact code; (3)
`related --tests` for which tests to run. Language-agnostic, offline, no API
key — safe for an agent to call freely.

---

## How the rounds connect

Round 1 questioned whether the graph earns its complexity (marginal value
~0.2–0.4 non-obvious files/commit). Round 2 confirmed the commit-time lever is
soft but the forensic `callers` reach is a real win. Round 3 validated the
decision that followed: cut the AST subsystem entirely, keep only co-change,
and reposition it agent-first as `related` — where the value is grep-blind
temporal couplings, not recall against a strawman. The v0.7.0 release gate
(above) is the green suite that shipped that decision.

---

## How to run tests

```bash
# Full suite (preferred — Makefile target)
make test
# equivalent to:
go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...

# Single package
go test ./application/...

# Single test by name
go test ./application/... -run TestCommitService_NoStagedChanges

# e2e only (TestMain builds the binary once; re-run after any source change)
go test ./e2e/...

# Verbose
go test ./... -v

# Build (no credentials)
make build
# or
go build -o git-agent .

# Format check (auto-runs on edit via hook; no golangci-lint configured)
gofmt -l .
```

`-count=1` is mandatory in the Makefile target — it disables test caching, so a
green run reflects the current source, not a cached result.

### e2e mechanics

`TestMain` in `e2e/` builds the `git-agent` binary once into a temp dir, then
every e2e test invokes it as a subprocess against a freshly-init temp git
repo. Consequences:
- After editing CLI source, re-run `go test ./e2e/...` — the binary is
  rebuilt, so stale results are impossible *within a fresh run*, but a binary
  left over from a previous run is not reused (TestMain rebuilds).
- e2e tests need no API key for the negative-path tests (NoAPIKey cases); the
  graph and commit happy-path tests use stubs, not real LLM calls.

---

## Test inventory

### e2e tests (`e2e/`)

| File | Covers |
|---|---|
| `commit_test.go` | `commit` flags (dry-run, intent, trailer, no-stage, amend), SIGINT, small-diff regression, JSON output, JSON dry-run, removed `--all`/`add` |
| `config_test.go` | `config set/get` hook, project/local scope, provider-key rejection, unknown key |
| `init_gitignore_test.go` | `init --gitignore` no-API-key/no-repo failures, `--force`, no-break-config-set |
| `init_graph_test.go` | `init` wizard does not build graph (opt-in; first commit does) |
| `init_test.go` | `init --scope` no-API-key failure, hook+scope together, `--max-commits`, `--force` |
| `related_test.go` | `related` reports co-change with linking commits, no-seeds emits empty JSON |
| `helpers_test.go` | shared e2e harness |

### BDD feature specs (17 files)

`application/cochange_index.feature`, `application/commit_graph_generation.feature`,
`application/commit_json.feature`, `application/commit_multi.feature`,
`application/impact_aggregation.feature`, `application/index_performance.feature`,
`application/scope_service.feature`,
`cmd/related.feature`, `cmd/related_output.feature`, `cmd/related_seeds.feature`,
`domain/commit/filelist.feature`, `domain/commit/model_coauthor_validator.feature`,
`domain/commit/validator.feature`, `hooks/conventional.feature`,
`infrastructure/diff/truncator.feature`, `infrastructure/graph/recency.feature`,
`infrastructure/openai/plan.feature`.

Each `.feature` has a paired `_test.go` that drives it.
