---
name: using-git-agent
description: Operates the git-agent CLI — atomic commits, init/config, the code graph (AST + co-change), and the audit event log. Use it whenever the user wants to commit or set up git-agent; when you are about to modify a feature and need the files that move with it (graph impact); when you are changing a function and want its callers/callees (graph callers/callees); when you need a symbol's location or source (graph symbol/search); when deciding which tests to run after a change (graph affected); when a test broke and you want to trace the action that introduced it (audit diagnose); or when you need action history (audit timeline) or a file's rename-aware provenance (audit provenance). All graph and audit queries are read-only and offline (no LLM, no API key); only commit and init --scope need a provider.
---

# Git Agent CLI

When this skill is loaded, pick the right git-agent command for the situation —
do **not** default to `git-agent commit`. Use the trigger table below to map
what is happening to the command that serves it.

## When to use git-agent

Reach for git-agent at these moments. Each situation maps to one command:

| Situation | Command |
|---|---|
| About to start multi-file work / modify a feature — find what else changes | `git-agent graph impact [files...]` |
| Changing a specific function/type — want its callers (who depends on it / blast radius) | `git-agent graph callers <symbol> --depth N` |
| Changing a function — want what it calls | `git-agent graph callees <symbol>` |
| Locate a symbol, see its source + one-hop neighbors | `git-agent graph symbol <name>` or `git-agent graph search <query>` |
| Deciding which tests to run after a change | `git-agent graph affected [files...]` |
| A test/regression broke — find the agent action that introduced it | `git-agent audit diagnose [symptom] --file <source>` |
| "What did the agent (or a human) change recently" / audit a session | `git-agent audit timeline` (`--file`/`--source`/`--since`) |
| Full history of one file, rename-aware, with out-of-band edits flagged | `git-agent audit provenance <file>` |
| Symbol is exported by an external package — where does this repo call into it | `git-agent graph external-refs` |
| Graph queries return nothing or look stale | `git-agent graph status` (reads auto-sync; if a full rebuild is needed, `git-agent init --graph`) |
| Suspect the Event Log was tampered with | `git-agent audit verify` |
| Ready to commit staged changes | `git-agent commit --intent "..."` |
| New repo, or no scopes configured | `git-agent init` (add `--graph` to also build the code graph now) |
| Provider / API key / model setup | `git-agent config show` / `config set <key> <value>` |

If the situation isn't listed, run `git-agent --help`, `git-agent graph --help`,
or `git-agent audit --help`. Every `graph` and `audit` query is read-only and
offline (no LLM, no API key); only `commit` and `init --scope` need a provider.

## Find related files before changing a feature

When you are about to modify a feature — or are partway through editing it — ask
the git graph which other files are related to the ones you are touching. Those
are the files most likely to also need updating (tests, callers, sibling modules)
and are easy to forget. Two complementary signals are available: file-level
co-change (`graph impact`) and symbol-level blast radius (`graph callers`).

### Co-change — files that historically change together

```
# Given the files of a feature, rank the files that usually change with them:
git-agent graph impact application/commit_service.go cmd/commit.go -o json

# Given a directory (a whole module/feature area):
git-agent graph impact infrastructure/hook -o json

# No arguments: use your CURRENT uncommitted edits as the seeds —
# "given what I've already changed, what else usually moves with it?"
git-agent graph impact -o json
```

Read the JSON to prioritise: each entry has `seed_matches` (how many of the seed
files it co-changes with — higher means more central to the feature),
`related_to` (which seeds), `coupling_strength`, and `score` (the ranking).
A file with `seed_matches` equal to the number of seeds is coupled to the whole
feature; open it before you finish. The first run auto-indexes git history;
queries are offline and need no LLM or API key.

### Symbol blast radius — who calls or references a symbol

When you know the function, struct, or type you're changing, walk the AST to
find what directly depends on it — no history needed. `graph callers <symbol>`
returns the incoming edges (who calls or references it); raise `--depth` to
widen the transitive blast radius. `graph callees <symbol>` is the inverse
(what the symbol itself calls).

```
# Direct callers/references of CommitService:
git-agent graph callers CommitService -o json

# Widen to transitive dependents two hops out:
git-agent graph callers CommitService --depth 2 -o json

# What the symbol itself depends on:
git-agent graph callees CommitService -o json
```

The JSON carries `symbol`, `direction`, `depth`, and a `results` array of
`{node, edge, depth}` entries. Use this when modifying a specific function or
type and you want to see exactly what would break.

### When to use which

| Question | Command |
|---|---|
| "I'm editing these files — what else usually moves?" | `graph impact` (co-change) |
| "I'm changing this function — what calls or references it?" | `graph callers <symbol> --depth N` |
| "What does this function itself depend on?" | `graph callees <symbol>` |

Use impact proactively at the start of multi-file work and again before
committing, so nothing coupled to the change is left behind.

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
| `3` | Graph not indexed — a `graph` read ran before the index was built (run `git-agent init --graph`, or let the next `commit` build it) |
| `4` | Event Log chain integrity broken (`audit verify` / `audit diagnose`) |

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
| `git-agent graph impact [path...]` | Rank files that historically change with the seeds (files, a directory, or — with no args — your working-tree changes). Co-change only. Finds the other files a feature change is likely to need. JSON via `-o json` |
| `git-agent graph callers <symbol>` | AST nodes that call or reference a symbol (incoming edges), up to `--depth` — the symbol's structural blast radius |
| `git-agent graph callees <symbol>` | AST nodes a symbol calls or references (outgoing edges), up to `--depth` |
| `git-agent graph symbol <name>` | Symbols matching the name: each one's location, signature, source snippet, and one-hop caller/callee trail. Returns `{"matches":[...]}`, one entry per match |
| `git-agent graph search <query>` | FTS5 prefix search over symbol name, qualified name, or signature (e.g. `process` matches `processData`); filter with `--kind` |
| `git-agent graph affected [files...]` | Test files transitively affected by changes to the given files (stdin: `git diff --name-only`) |
| `git-agent graph external-refs` | List every call/field site where this repo reaches into an external (non-indexed) package. The answer `callers`/`search` cannot give — they only walk the resolved AST edge graph |
| `git-agent graph status` | Show graph index health and row counts (commits, files, authors, co-change pairs, sessions, actions) |
| `git-agent audit timeline` | Show recent agent/human action history (sessions, tools, files); filter with `--file`, `--source`, `--since` |
| `git-agent audit diagnose [symptom] --file <source>` | Trace a failing symptom to the agent action that most likely introduced it (suspect window + co-change + ranking). `--file <source>` seeds the relevant file set (effectively required for candidates); `[symptom]` is optional context. Add `--llm` to re-rank candidates via the configured diagnose LLM |
| `git-agent audit provenance <file>` | Rename-aware change history for one file: every captured change plus out-of-band changes, folding in pre-rename identities |
| `git-agent audit verify` | Walk the hash-chained Event Log and verify it has not been tampered with. Exits 4 on a break |
| `git-agent capture` | Record an agent action into the graph. Designed to run as a Claude Code PostToolUse hook (installed via `init --agent-hook`). Hidden from `--help` |
| `git-agent init` | Initialize git-agent in a repo (generates scopes, .gitignore, installs hooks) |
| `git-agent init --graph` | One-shot cold start: build the full code graph (commit-history co-change + Event-Log projections + AST index). No LLM needed. Otherwise the first `commit` builds it automatically |
| `git-agent init --agent-hook` | Install the Claude Code PostToolUse hook so agent edits are auto-captured into the graph |
| `git-agent init --scope` | Regenerate scopes only |
| `git-agent init --user --hook <value>` | Configure a hook in user-level config (`~/.config/git-agent/config.yml`), independent of any project config |
| `git-agent config show` | Show resolved provider configuration |
| `git-agent config set <key> <value>` | Set a config value (auto-selects scope) |
| `git-agent config get <key>` | Show a config value and its source scope |
| `git-agent completion <shell>` | Generate shell completions (bash/zsh/fish/powershell) |
| `git-agent version` | Print build version |

## CLI reference

Full command reference (all flags, subcommands, config scopes, hook types): [references/cli.md](references/cli.md)
