---
name: use-git-agent
description: Operates the git-agent CLI — atomic commits, init/config, and the code graph (event log + AST + co-change). Use it whenever the user wants to commit or set up git-agent; when you are about to modify a feature and need the files that move with it (graph impact); when you are changing a function and want its callers/callees (graph callers/callees); when you need a symbol's location or source (graph node/query); when deciding which tests to run after a change (graph affected); when a test broke and you want to trace the action that introduced it (graph diagnose); or when you need action history (graph timeline) or a file's rename-aware provenance (graph provenance). All graph queries are read-only and offline (no LLM, no API key); only commit and init --scope need a provider.
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
| Changing a specific function/type — want its callers (who depends on it) | `git-agent graph impact --symbol <name>` or `git-agent graph callers <symbol>` |
| Changing a function — want what it calls | `git-agent graph callees <symbol>` |
| Locate a symbol, see its source + one-hop neighbors | `git-agent graph node <name>` or `git-agent graph query <search>` |
| Deciding which tests to run after a change | `git-agent graph affected [files...]` |
| A test/regression broke — find the agent action that introduced it | `git-agent graph diagnose [symptom] --file <source>` |
| "What did the agent (or a human) change recently" / audit a session | `git-agent graph timeline` (`--file`/`--source`/`--since`) |
| Full history of one file, rename-aware, with out-of-band edits flagged | `git-agent graph provenance <file>` |
| Graph queries return nothing or look stale | `git-agent graph status` → `git-agent graph sync` (or `git-agent graph index` for a full rebuild) |
| Suspect the Event Log was tampered with | `git-agent graph verify` |
| Ready to commit staged changes | `git-agent commit --intent "..."` |
| New repo, or no scopes configured | `git-agent init` |
| Provider / API key / model setup | `git-agent config show` / `config set <key> <value>` |

If the situation isn't listed, run `git-agent --help` or `git-agent graph --help`.
Every `graph` query is read-only and offline (no LLM, no API key); only `commit`
and `init --scope` need a provider.

## Find related files before changing a feature

When you are about to modify a feature — or are partway through editing it — ask
the git graph which other files are related to the ones you are touching. Those
are the files most likely to also need updating (tests, callers, sibling modules)
and are easy to forget. Two analysis modes are available:

### Co-change mode (default) — files that historically change together

```
# Given the files of a feature, rank the files that usually change with them:
git-agent graph impact application/commit_service.go cmd/commit.go --json

# Given a directory (a whole module/feature area):
git-agent graph impact infrastructure/hook --json

# No arguments: use your CURRENT uncommitted edits as the seeds —
# "given what I've already changed, what else usually moves with it?"
git-agent graph impact --json
```

Read the JSON to prioritise: each entry has `seed_matches` (how many of the seed
files it co-changes with — higher means more central to the feature),
`related_to` (which seeds), `coupling_strength`, and `score` (the ranking).
A file with `seed_matches` equal to the number of seeds is coupled to the whole
feature; open it before you finish. The first run auto-indexes git history;
queries are offline and need no LLM or API key.

### Structural mode — symbols that call or reference a given symbol

When you know the function, struct, or type you're changing, structural mode
walks the AST to find direct callers and references (incoming edges — who
depends on it) — no history needed. Pass `--symbol <name>` (mode defaults to
`structural`):

```
# Find all symbols structurally linked to CommitService:
git-agent graph impact --symbol CommitService --json

# Combine both signals — co-change AND structural — for the richest view:
git-agent graph impact --symbol CommitService --mode combined --json
```

The JSON shape is different from co-change: a `seed_node` object (the symbol
with its `kind`, `qualified_name`, `file_path`, lines, columns, `is_exported`,
`return_type`) and an `impacted` array of structurally connected symbols. Use
this when modifying a specific function or type and you want to see what
directly depends on it.

### When to use which mode

| Question | Mode |
|---|---|
| "I'm editing these files — what else usually moves?" | `cochange` (default) |
| "I'm changing this function — what calls or references it?" | `--symbol <name>` (structural) |
| "Give me everything — history and AST" | `--symbol <name> --mode combined` |

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

Hook exit codes: `0` = allow, non-zero = block.

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
| `git-agent graph impact [path...]` | Rank files that historically change with the seeds (files, a directory, or — with no args — your working-tree changes). Finds the other files a feature change is likely to need. JSON via `--json` |
| `git-agent graph impact --symbol <name>` | AST-structural impact: symbols that call or reference the given symbol (incoming edges — who depends on it). `--mode combined` returns co-change + structural as separate fields |
| `git-agent graph timeline` | Show recent agent/human action history (sessions, tools, files); filter with `--file`, `--source`, `--since` |
| `git-agent graph diagnose [symptom] --file <source>` | Trace a failing symptom to the agent action that most likely introduced it (suspect window + co-change + ranking). `--file <source>` seeds the relevant file set (effectively required for candidates); `[symptom]` is optional context. Add `--llm` to re-rank candidates via the configured diagnose LLM |
| `git-agent graph provenance <file>` | Rename-aware change history for one file: every captured change plus out-of-band changes, folding in pre-rename identities |
| `git-agent graph status` | Show graph index health and row counts (commits, files, authors, co-change pairs, sessions, actions) |
| `git-agent graph verify` | Walk the hash-chained Event Log and verify it has not been tampered with. Exits 4 on a break |
| `git-agent graph index` | Build/refresh all derived indexes: replay the Event Log into projections (sessions, actions, co-change) and ensure the AST index. `--reindex` forces a full AST re-index |
| `git-agent graph sync` | Bring projections up to date with the Event Log (no-op when already current; otherwise incrementally replays only the new events) |
| `git-agent graph callers <symbol>` | AST nodes that call or reference a symbol (incoming edges), up to `--depth` |
| `git-agent graph callees <symbol>` | AST nodes a symbol calls or references (outgoing edges), up to `--depth` |
| `git-agent graph node <name>` | Symbols matching the name: each one's location, signature, source snippet, and one-hop caller/callee trail. Returns an array (one entry per match) |
| `git-agent graph query <search>` | FTS5 prefix search over symbol name, qualified name, or signature (e.g. `process` matches `processData`); filter with `--kind` |
| `git-agent graph affected [files...]` | Test files transitively affected by changes to the given files (stdin: `git diff --name-only`) |
| `git-agent capture` | Record an agent action into the graph. Designed to run as a Claude Code PostToolUse hook (installed via `init --agent-hook`). Hidden from `--help` |
| `git-agent init` | Initialize git-agent in a repo (generates scopes, .gitignore, installs hooks) |
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
