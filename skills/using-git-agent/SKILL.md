---
name: using-git-agent
description: Operates the git-agent CLI — atomic AI commits plus co-change relations for agents, all-language, offline, no API key. Use it whenever the user wants to commit or set up git-agent; when you are about to modify a feature and need the files that historically move with it, with the commits that explain the coupling (related); when deciding which tests to run after a change (related --tests); or when checking the co-change index health (status). All related and status queries are read-only and offline (no LLM, no API key); only commit and init --scope need a provider.
---

# Git Agent CLI

When this skill is loaded, pick the right git-agent command for the situation —
do **not** default to `git-agent commit`. Use the trigger table below to map
what is happening to the command that serves it.

## When to use git-agent

Reach for git-agent at these moments. Each situation maps to one command:

| Situation | Command |
|---|---|
| About to start multi-file work / modify a feature — find what else changes | `git-agent related [files...]` |
| Deciding which tests to run after a change | `git-agent related <files...> --tests` |
| Co-change queries return nothing or look stale | `git-agent status` (reads auto-sync; if a full rebuild is needed, `git-agent init --graph`) |
| Ready to commit staged changes | `git-agent commit --intent "..."` |
| New repo, or no scopes configured | `git-agent init` (add `--graph` to also build the code graph now) |
| Provider / API key / model setup | `git-agent config show` / `config set <key> <value>` |

If the situation isn't listed, run `git-agent --help`. Every `related` and
`status` query is read-only and offline (no LLM, no API key); only `commit`
and `init --scope` need a provider.

## Find related files before changing a feature

When you are about to modify a feature — or are partway through editing it — ask
git-agent which other files historically change together with the ones you are
touching. Those are the files most likely to also need updating (tests, sibling
modules, config) and are easy to forget. `git-agent related` mines git history
(not source parsing), so it is language-agnostic, offline, and needs no API key;
the first run auto-indexes.

```
# Given the files of a feature, rank the files that usually change with them:
git-agent related application/commit_service.go cmd/commit.go -o json

# Given a directory (a whole module/feature area):
git-agent related infrastructure/hook -o json

# No arguments: use your CURRENT working-tree changes as the seeds —
# "given what I've already changed, what else usually moves with it?"
git-agent related -o json

# Keep only related test files — "which tests should I run for this change?"
git-agent related application/commit_service.go --tests
```

Read the JSON to prioritise: each related file has `seed_matches` (how many of
the seed files it co-changes with — higher means more central to the feature),
`related_to` (which seeds), `coupling_strength`, `score` (the ranking), and a
`commits` array of `{sha, subject, ts}` — the actual commits that changed the
files together, i.e. the evidence for *why* they are related. Read those
subjects to judge whether a coupling is real or incidental. A file with
`seed_matches` equal to the number of seeds is coupled to the whole feature;
open it before you finish.

Useful flags: `--depth` and `--top` shape how far and how many results come
back, `--min-count` filters out weak couplings, `--tests` narrows the result to
related test files ("which tests to run"), and `--reindex` forces a fresh
history scan.

Use `related` proactively at the start of multi-file work and again before
committing, so nothing coupled to the change is left behind.

## Pair `related` with your own search tools (Grep / Glob / Explore)

`related` does not replace your built-in code search — it covers a blind spot
in it. Grep, Glob, and the Explore agent find files by their **current content
and symbols** (spatial: "where is `X` referenced *now*?"). `related` finds
files by **how they have changed together** (temporal: "what moves with `X`,
and why?"). The two are complementary, so run both.

End-to-end measurement on real repos makes the gap concrete. For a seed file,
many of its strongest co-change partners carry **no textual link** a symbol
search would catch:

- In `gin`, of `context.go`'s top co-change partners, more than half are
  grep-blind — `tree.go`, `errors.go`, `binding/*`, `render/*` — none mention
  the `Context` symbol or the filename.
- In `flask`, `app.py` co-changes with `CHANGES.rst` (85 commits) and
  `docs/templating.rst`. A coding agent relying on grep alone would never learn
  it must also update the changelog and the docs when it edits `app.py`.

The `commits` array is the other thing static search cannot give you: it is the
**intent** behind a coupling ("these two moved together in *fix
subdomain_matching=False behavior*"), so you can judge whether the link is
architectural or incidental.

Recommended loop for a coding agent:

1. `git-agent related <file>` — get the blast radius plus the commits that
   explain *why* each file is coupled (context Grep can't give).
2. Grep / Read / Explore those files — get exact symbol locations and code
   (the spatial detail `related` can't give).
3. `git-agent related <file> --tests` — get which tests to run before you stop.

Because it is offline, zero-cost, and answers in milliseconds, call `related`
freely — it is safe to run on every multi-file task.

**Trust calibration.** Co-change is an *aggregate* signal. It is accurate for
consistent couplings (an implementation and its test almost always move
together) and softer for feature-spanning changes: a one-off feature that
touched files across packages, or a sweeping commit like "support go1.18", will
link files that are not really coupled. Don't treat the ranking as ground
truth — read the `commits` subjects. A partner backed by focused, on-topic
commits is a real coupling; one backed only by a single mass-refactor commit is
probably noise.

## Commit workflow

1. **Intent** — derive a one-sentence intent from the conversation. If no signal exists, run `git diff --stat` to understand what changed, then form the intent from that.

2. **Commit** — run:
   ```
   git-agent commit --intent "..."
   ```
   No provider flags on the first attempt.

3. **On auth error (401 / missing key)** — retry once with `--free`:
   ```
   git-agent commit --intent "..." --free
   ```

4. **If `--free` also fails** — guide the user to create `~/.config/git-agent/config.yml`:
   ```yaml
   base_url: https://api.openai.com/v1
   api_key: sk-...
   model: gpt-4o
   ```
   Other supported providers: Cloudflare Workers AI, local Ollama.

5. **On planner timeout** (`LLM planner timed out (model=..., after ...)`) — the
   diff was too large, or the model too slow, to plan the commit groups in time.
   `--request-timeout` is **not** a flag; raise the budget via the config key,
   then retry:
   ```
   git-agent config set request_timeout 5m
   ```
   Or shrink what the planner has to reason about: sharpen `--intent`, cap the
   diff with `--max-diff-lines <n>`, or switch to a more capable model via
   `--model`.

### Structured output (`-o json`)

When you need to read the result back programmatically (which commits were
created, their SHAs, whether a hook ran), add `-o json`:

```
git-agent commit --intent "..." -o json
```

It prints a single object:

```json
{
  "dry_run": false,
  "commits": [
    {"title": "...", "message": "...", "files": ["..."], "sha": "...", "hook_outcome": "passed"}
  ],
  "committed_count": 1,
  "final_sha": "..."
}
```

`hook_outcome` is `passed` (a hook ran and accepted the commit) or `skipped`
(no validating hook). On `--dry-run`, `committed_count` is `0` and the `sha`
fields are empty. `commit` defaults to human-readable text; pass `-o json` only
when scripting.

## Do not track `.git-agent/graph.db`

The graph database (`.git-agent/graph.db`) is generated at runtime and must
**never** be tracked in git. If it is tracked, `.gitignore` has no effect and
every run re-modifies it, producing a stream of `chore: update graph database
file` commits — the "infinite recreation" loop.

`git-agent init` handles this by default (full wizard and `--gitignore`):
  1. Writes `.git-agent/graph.db` (+ `*.db-shm`/`*.db-wal`/`*.db-journal`) into
     `.gitignore`, idempotently.
  2. If `.git-agent/graph.db` is already tracked, runs `git rm --cached` on it
     so the ignore rule can take effect (prints `Untracked ... graph.db`).

So after `git-agent init`, the file is ignored and untracked automatically.
Verify when in doubt:

```
git ls-files .git-agent/graph.db        # must print nothing (untracked)
git check-ignore .git-agent/graph.db    # prints the path, exit 0 (ignored)
```

If a repo predates this init behavior and `git ls-files` still shows the path,
re-run `git-agent init --gitignore` to untrack it. Never
`git add -f .git-agent/graph.db`. If a commit shows
`graph.db | Bin ... -> ... bytes`, the file is tracked again — untrack it
before continuing (raw `git rm --cached .git-agent/graph.db` + commit; git-agent's
planner cannot stage a pure deletion).

## Useful flags

| Flag | When to use |
|---|---|
| `--dry-run` | User wants to preview the message without committing |
| `--no-stage` | User has already staged specific files and doesn't want auto-staging |
| `--amend` | User wants to rewrite the most recent commit message |
| `--intent "..."` | Always set — keeps generated messages focused |
| `--co-author "Name <email>"` | Attribute a co-author (repeatable); skipped if `no_model_co_author` is set in config |
| `--trailer "Key: Value"` | Add an arbitrary git trailer (repeatable) |
| `--no-attribution` | Omit the default `Co-Authored-By: Git Agent` trailer |
| `--max-diff-lines N` | Cap diff size sent to the model (0 = no limit) |
| `-o json` | Emit machine-readable commit results (titles, SHAs, hook outcomes) for scripting; defaults to text |

`--amend` and `--no-stage` are mutually exclusive.

## Multi-commit splitting

git-agent automatically splits staged changes into multiple atomic commits (up to 5 groups) when the AI planner detects logically distinct changes. Each group is staged, committed, and hook-validated separately. No user action is needed — this is the default behavior.

## Auto-scope

If no scopes are configured for the project, git-agent generates scopes from git history automatically before planning. Each scope is a structured object with a `name` and an optional `description` (used as LLM context during commit message generation). To trigger scope generation manually: `git-agent init --scope`.

## Require model co-author

Set `require_model_co_author: true` in `.git-agent/config.yml` (or user / local scope) to enforce that every commit carries a `Co-Authored-By` trailer from a known AI-provider domain. The default Git Agent attribution trailer alone does not satisfy this — only domains in `anthropic.com`, `openai.com`, `google.com`, plus anything listed under `model_co_author_domains:`, count.

When the flag is on, callers **must** pass `--co-author "Model Name <email@domain>"` explicitly. `git-agent` validates this at the CLI layer before invoking the LLM and exits with code `1` and a clear hint if the flag is missing — the model itself is never relied on to produce the trailer (it gets the casing or placement wrong). Example:

```
git-agent commit --co-author "Claude Opus 4.7 <noreply@anthropic.com>"
```

## Hook failures

If the commit is blocked (exit code `2`), retry with a more specific `--intent`:

```
git-agent commit --intent "update module path"
```

Hook exit codes (the hook script's own contract): `0` = allow, non-zero = block.

## Exit codes

`git-agent` itself uses a typed exit-code taxonomy across all commands:

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | General error (no API key, git error, no changes, etc.) |
| `2` | Commit blocked by a hook after retries |
| `3` | Retired / unused — formerly "graph not indexed"; no longer emitted (co-change reads auto-index on first run) |
| `4` | Retired / unused — formerly "Event Log chain integrity"; the Event Log subsystem has been removed and this code is no longer emitted |

## Commit format

```
<type>(<scope>): <description>

- <Bullet one>
- <Bullet two>

<Explanation paragraph>

Co-Authored-By: Git Agent <noreply@git-agent.dev>
```

- Title: lowercase, ≤50 chars, no period
- Bullets: uppercase first letter, imperative mood, ≤72 chars per bullet; LLM generates as a JSON array — trailers never enter LLM context
- Explanation: required, sentence case; lines >100 chars are wrapped to ~72 chars
- Terminal output shows only the explanation paragraph (bullets appear in the git commit body but not in the CLI output)

## Other commands

| Command | What it does |
|---|---|
| `git-agent related [path...]` | Rank files that historically change with the seeds (files, a directory, or — with no args — your working-tree changes); in JSON each result carries a `commits` array as the evidence for the coupling. Add `--tests` to keep only related test files. Finds the other files a feature change is likely to need. JSON via `-o json` |
| `git-agent status` | Show co-change index health and row counts (commits, files, authors, co-change pairs, last indexed commit, db size) |
| `git-agent init` | Initialize git-agent in a repo (generates scopes, .gitignore, installs hooks) |
| `git-agent init --graph` | One-shot cold start: build the code graph (commit-history co-change). No LLM needed. Otherwise the first `commit` builds it automatically |
| `git-agent init --scope` | Regenerate scopes only |
| `git-agent init --user --hook <value>` | Configure a hook in user-level config (`~/.config/git-agent/config.yml`), independent of any project config |
| `git-agent config show` | Show resolved provider configuration |
| `git-agent config set <key> <value>` | Set a config value (auto-selects scope) |
| `git-agent config get <key>` | Show a config value and its source scope |
| `git-agent completion <shell>` | Generate shell completions (bash/zsh/fish/powershell) |
| `git-agent version` | Print build version |

## CLI reference

Full command reference (all flags, subcommands, config scopes, hook types): [references/cli.md](references/cli.md)
