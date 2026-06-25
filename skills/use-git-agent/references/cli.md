# git-agent CLI Reference

## Root

```
git-agent [command] [flags]
```

AI-first Git CLI for automated commit message generation.

### Global Flags

| Flag | Type | Description |
|------|------|-------------|
| `--api-key` | string | API key for the AI provider |
| `--base-url` | string | Base URL for the AI provider |
| `--model` | string | Model to use for generation |
| `--free` | bool | Use only build-time embedded credentials; ignore config file and git config |
| `--verbose`, `-v` | bool | Verbose output |

`--free` is mutually exclusive with `--api-key`, `--model`, and `--base-url` (enforced at parse time by Cobra).

### Provider config resolution (highest to lowest priority)

1. Global CLI flags (`--api-key`, `--model`, `--base-url`)
2. `git config --local git-agent.{model,base-url}`
3. `~/.config/git-agent/config.yml` (supports `$ENV_VAR` expansion)
4. Build-time defaults

---

## git-agent commit

```
git-agent commit [flags]
```

Generate and create commit(s) with AI-generated messages. Auto-stages all changes, auto-generates scopes if none are configured, and splits changes into atomic commits via LLM planning.

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--intent` | string | | Describe the intent of the change |
| `--dry-run` | bool | false | Print commit message without committing |
| `--no-stage` | bool | false | Skip auto-staging; only commit already-staged changes |
| `--amend` | bool | false | Regenerate and amend the most recent commit |
| `--co-author` | stringArray | | Add a co-author trailer (repeatable); skipped if `no_model_co_author` is set in config |
| `--trailer` | stringArray | | Add an arbitrary git trailer, format `"Key: Value"` (repeatable) |
| `--no-attribution` | bool | false | Omit the default `Co-Authored-By: Git Agent <noreply@git-agent.dev>` trailer (`--no-git-agent` is a deprecated alias) |
| `--max-diff-lines` | int | 0 | Maximum diff lines to send to the model (0 = no limit) |

`--amend` and `--no-stage` are mutually exclusive (enforced at parse time by Cobra).

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (no API key, git error, etc.) |
| `2` | Commit blocked by hook after retries |

### Multi-commit splitting

The AI planner can split staged changes into multiple atomic commits (max 5 commit groups per run). For each group, git-agent unstages all files, re-stages only the group's files, generates a message, and commits.

### Hook retry logic

- 3 retries per commit group
- 2 re-plans maximum if retries are exhausted
- After all retries and re-plans fail: exits with code `2`

### Auto-scope

If no scopes are configured (project config is nil or has empty scopes), git-agent automatically generates scopes from git history before planning. Each scope is a structured object (`{"name": "...", "description": "..."}`) — the description provides LLM context during commit message generation. If any planned commit group lacks a scope, scopes are refreshed once and the plan is regenerated.

---

## git-agent init

```
git-agent init [flags]
```

Initialize git-agent in the current repository.

With no flags, runs the full setup wizard:
1. Ensures a git repo exists (runs `git init` if needed)
2. Generates `.gitignore` via AI
3. Generates commit scopes from git history via AI
4. Writes `.git-agent/config.yml` with scopes and `hook: [conventional]`

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--scope` | bool | false | Generate scopes via AI |
| `--gitignore` | bool | false | Generate `.gitignore` via AI |
| `--hook` | stringArray | | Hook to configure: `conventional`, `empty`, or a file path (repeatable) |
| `--force` | bool | false | Overwrite existing config/`.gitignore` |
| `--max-commits` | int | 200 | Max commits to analyze for scope generation |
| `--local` | bool | false | Write config to `.git-agent/config.local.yml` (requires an action flag) |
| `--user` | bool | false | Write config to `~/.config/git-agent/config.yml` (requires `--hook`; mutually exclusive with `--local`) |

`--local` without an action flag (`--scope`, `--gitignore`, or `--hook`) returns an error.

`--user` requires `--hook` and cannot be combined with `--scope` or `--gitignore`. Use it to configure a user-level hook preference independent of any project config.

### Hook types

| Value | Behavior |
|-------|----------|
| `conventional` | Go-native Conventional Commits 1.0.0 validation |
| `empty` | No-op; always passes |
| `<file path>` | Go validation + shell script at that path |

Shell hooks receive a JSON payload on stdin with the following fields:

```json
{
  "diff": "...",
  "commitMessage": "feat(cli): add feature\n\n- Detail one\n- Detail two\n\nExplanation.",
  "intent": "add new feature",
  "stagedFiles": ["cmd/feature.go", "cmd/feature_test.go"],
  "config": {
    "scopes": [{"name": "cli", "description": "CLI commands and flags"}],
    "hooks": ["conventional"],
    "maxDiffLines": 0,
    "noGitAgentCoAuthor": false,
    "noModelCoAuthor": false
  }
}
```

Scopes are structured objects with `name` and `description` fields. Plain strings (e.g., `"cli"`) are accepted for backward compatibility during JSON unmarshaling.

Exit 0 = allow, non-zero = block.

To reconfigure hooks after init: `git-agent config set hook <value>` (when setting a file path, the script is copied to `.git-agent/hooks/pre-commit` automatically)

---

## git-agent config

```
git-agent config [command]
```

Manage git-agent configuration.

### Subcommands

| Command | Description |
|---------|-------------|
| `show` | Show resolved provider configuration |
| `set <key> <value>` | Set a configuration value |
| `get <key>` | Show the resolved value of a configuration key |

---

## git-agent config show

```
git-agent config show [flags]
```

Show the resolved provider configuration (`api_key` masked, `model`, `base_url`). Respects global flags — pass `--api-key`/`--model`/`--base-url` to preview what the resolved config would look like with those overrides.

Output format:
```
api_key:  sk-****
model:    claude-3-5-haiku-20241022
base_url: https://api.anthropic.com/v1
```

---

## git-agent config set

```
git-agent config set <key> <value> [flags]
```

Set a configuration value in the specified scope. Keys accept both snake_case and kebab-case forms.

### Key aliases (kebab-case → snake_case)

| Kebab-case | Canonical |
|------------|-----------|
| `api-key` | `api_key` |
| `base-url` | `base_url` |
| `max-diff-lines` | `max_diff_lines` |
| `no-git-agent-co-author` | `no_git_agent_co_author` |
| `no-model-co-author` | `no_model_co_author` |

### Scopes

| Flag | File | Purpose |
|------|------|---------|
| `--user` | `~/.config/git-agent/config.yml` | Provider keys: `api_key`, `base_url`, `model` |
| `--project` | `.git-agent/config.yml` | Shared, checked into git |
| `--local` | `.git-agent/config.local.yml` | Personal override, gitignored |

When no scope flag is given, provider keys default to `--user`; all others default to `--project`.

`--user`, `--project`, and `--local` are mutually exclusive (enforced at parse time by Cobra).

---

## git-agent config get

```
git-agent config get <key> [flags]
```

Show the resolved value of a configuration key and its source scope. Accepts both snake_case and kebab-case keys.

Resolution order for non-provider keys: local > project > user. Provider-only keys (api_key, base_url, model) resolve from user scope only.

---

## git-agent completion

```
git-agent completion [bash|zsh|fish|powershell]
```

Generate shell completion scripts for git-agent.

### Loading completions

**Bash:**
```bash
source <(git-agent completion bash)
# Persist (Linux):
git-agent completion bash > /etc/bash_completion.d/git-agent
# Persist (macOS):
git-agent completion bash > $(brew --prefix)/etc/bash_completion.d/git-agent
```

**Zsh:**
```bash
# Enable completion if not already:
echo "autoload -U compinit; compinit" >> ~/.zshrc
# Install:
git-agent completion zsh > "${fpath[1]}/_git-agent"
```

**Fish:**
```bash
git-agent completion fish | source
# Persist:
git-agent completion fish > ~/.config/fish/completions/git-agent.fish
```

**PowerShell:**
```powershell
git-agent completion powershell | Out-String | Invoke-Expression
# Persist:
git-agent completion powershell >> $PROFILE
```

---

## git-agent version

```
git-agent version
```

Print the build version (injected via ldflags; defaults to `dev` in local builds).

---

## git-agent graph impact

```
git-agent graph impact [path...] [--symbol <name>] [--mode <mode>]
```

Find files or symbols related to the given seeds. Three modes are available:

| Mode | Trigger | What it returns |
|---|---|---|
| `cochange` (default) | Seeds are file paths (or none = working-tree changes) | Files that historically change with the seeds |
| `structural` | `--symbol <name>` (auto-selected when `--symbol` is given) | AST symbols that call or reference the seed symbol (incoming edges — who depends on it) |
| `combined` | `--symbol <name> --mode combined` | Co-change and structural results returned as separate `co_change` and `structural` fields |

Seeds for co-change are one or more files, a directory (expands to its tracked
files), or — with **no arguments** — the current working-tree changes ("given
what I've edited, what else usually moves?"). Co-change neighbours are
aggregated across all seeds, so a file coupled to several seeds ranks above one
coupled to a single seed. The first run auto-indexes git history; queries are
offline (no LLM / API key). Tooling directories (`.git-agent/`, `.claude/`) are
never used as seeds.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--symbol <name>` | | Query structural impact by symbol name (auto-selects `structural` mode) |
| `--mode <mode>` | `cochange` (or `structural` if `--symbol` given) | Impact mode: `structural`, `combined`, or `cochange` |
| `--depth N` | 1 | Transitive co-change depth; depth > 1 entries are marked `[indirect, depth N]` |
| `--top N` | 20 | Max results |
| `--min-count N` | 3 | Minimum co-change count to include (index floor is 2; values below 2 cannot surface more) |
| `--reindex` | false | Force a full re-index before querying |
| `--json` / `--text` | auto | Force output format (default: JSON when piped, text on a TTY) |

### Output — cochange mode

Text shows `path  strength%  (N co-changes)`, with `[M/T seeds: ...]` when more
than one seed. JSON fields per entry: `path`, `coupling_count`,
`coupling_strength`, `score` (sum of strengths over matched seeds — the rank
key), `seed_matches`, `related_to` (which seeds), `depth`. Top-level: `targets`,
`co_changed`, `total_found`, `query_ms`.

### Output — structural / combined mode

JSON shape is different. Top-level: `seed_node` (the queried symbol with `id`,
`kind`, `name`, `qualified_name`, `file_path`, `language`, `start_line`,
`end_line`, `start_column`, `end_column`, `is_exported`, `return_type`,
`updated_at`), `impacted` (array of structurally connected symbols with the
same shape), `total_found`, `query_ms`. The `seed_node` is always present even
when `impacted` is empty (symbol not found returns `{"error": "symbol ... not found"}`).

```bash
git-agent graph impact application/commit_service.go cmd/commit.go --json
git-agent graph impact internal/auth                                  # a whole module
git-agent graph impact                                                # seeds = my current edits
git-agent graph impact --symbol CommitService --json                  # structural
git-agent graph impact --symbol CommitService --mode combined --json  # both signals
```

---

## git-agent graph timeline

```
git-agent graph timeline [--file <path>] [--source <src>] [--since <2h|7d|RFC3339>] [--top N] [--json|--text]
```

Show recent agent/human action history grouped into sessions, with the tool and
files for each action. Populated by `git-agent capture` (see below). Offline.

## git-agent graph status

```
git-agent graph status [--json|--text]
```

Snapshot of graph index health: whether the index exists, the last indexed
commit, and row counts. Read-only. The first `graph impact` run auto-indexes
git history, so `commit_count`/`file_count`/`author_count`/`co_changed_count`
stay 0 until then.

### Fields

`exists`, `last_indexed_commit` (omitempty — absent until git history is
indexed by the first `graph impact`), `commit_count`, `file_count`,
`author_count`, `co_changed_count`, `session_count`, `action_count`,
`db_size_bytes` (SQLite page_count × page_size).

## git-agent graph verify

```
git-agent graph verify [--json|--text]
```

Walk the hash-chained Event Log and verify it has not been tampered with:
recompute each event's `this_hash`, follow the genesis `prev_hash` linkage, and
check seq continuity. Read-only. **Exits 4** on any integrity break.

### Fields

`Status` (`ok`/`broken`), `EventsTotal`, `EventsVerified`, `FirstBreak`
(`{Kind, Seq, EventID, ExpectedThisHash, StoredThisHash}` — null when clean).

## git-agent graph index

```
git-agent graph index [--reindex]
```

Build (or refresh) every derived index — the codegraph `index` analogue:
1. Verify the hash-chained Event Log, then rebuild the event-log projections
   (sessions, actions, event_files, co-change) by replaying it, reconciling
   unexplained working-tree changes into out-of-band Events, and replaying
   again when any were appended.
2. Ensure the AST index is fresh against the current working tree (the
   symbol/call-graph index that `graph impact --symbol` / `callers` / `callees`
   / `node` / `query` / `affected` read). `--reindex` forces a full AST re-index.

Mutates only derived tables; the append-only Event Log is never touched. No
`--json` flag (status line only). Use `graph sync` for a no-op-when-current
refresh of just the event-log projections.

## git-agent graph sync

```
git-agent graph sync [--json|--text]
```

Bring the derived projections up to date with the Event Log. A no-op when the
projections already reflect the latest event seq (`max_projected_seq >=
max_event_seq`); otherwise INCREMENTALLY replays only the new events (no reset —
appends to existing projections) and reconciles unexplained working-tree
changes, folding any out-of-band Events appended. Use `sync` for the common
refresh; use `index` to force a full reset-and-rebuild. The incremental fold
reuses the same state machine as `index`, so `sync` produces projections
byte-identical to `index` over the same Event Log.

### Fields

`max_event_seq`, `max_projected_seq` (pre-sync), `up_to_date` (was already
current / no-op), `replayed` (an incremental replay ran), `out_of_band_appended`.

## git-agent graph provenance

```
git-agent graph provenance <file> [--json|--text]
```

Reconstruct a file's full, rename-aware chronological history from the Event
Log. `ResolveRenames` folds in the file's pre-rename identities, then a single
ordered read over `event_files` covers both observed and out-of-band changes.
Out-of-band rows (source `unknown` — content no captured action explains) are
flagged. Read-only.

### Fields

`File`, `Rows[]`: `{Seq, When, Who, Tool, BeforeBlob, AfterBlob, ChangeKind,
LinkedCommit, OutOfBand}`.

## git-agent graph callers

```
git-agent graph callers <symbol> [--depth N] [--reindex] [--json|--text]
```

AST nodes that call or reference the given symbol (incoming edges), traversed
up to `--depth`. The inverse of `callees`. Auto-indexes the AST on first run.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--depth N` | 1 | Transitive traversal depth |
| `--reindex` | false | Force a full AST re-index before query |
| `--json` / `--text` | auto | Force output format |

### Fields

`symbol`, `direction`, `depth`, `results[]` (`{node, edge, depth}`), `total`.

## git-agent graph callees

```
git-agent graph callees <symbol> [--depth N] [--reindex] [--json|--text]
```

AST nodes the given symbol calls or references (outgoing edges), traversed up to
`--depth`. The inverse of `callers`. Same flags and output shape as `callers`
(with `direction: "callees"`).

## git-agent graph node

```
git-agent graph node <name> [--reindex] [--json|--text]
```

Look up AST symbols by name and print each one's kind, file, line range, and
signature, plus a source snippet read from the working tree and the one-hop
caller/callee trails. Returns an **array** (one entry per matching symbol —
names can be ambiguous). Auto-indexes the AST on first run.

### Fields (per entry)

`node` (full `ASTNode`), `source` (snippet, omit if unavailable), `callers[]`,
`callees[]` (`{node, edge, depth}`).

## git-agent graph query

```
git-agent graph query <search> [--kind <k>] [--reindex] [--json|--text]
```

FTS5 **prefix** search over indexed symbols: matches the `name`,
`qualified_name`, or `signature` columns (e.g. `process` matches
`processData`; `data` does not — it is not a name prefix). `bm25`-ranked.
Filter by node kind with `--kind` (e.g. `function`, `method`, `type`).

### Fields

`query`, `results[]` (`{Node, Score}`), `total`.

## git-agent graph affected

```
git-agent graph affected [files...] [--depth N] [--reindex] [--json|--text]
```

Trace transitive dependents of the symbols declared in the changed files and
filter them to test files (Go: `*_test.go`). The inverse question of `impact`:
given what I changed, which tests should I run? With no file args and stdin
piped, reads `git diff --name-only` from stdin; with no args and a TTY, uses
the working-tree changes.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--depth N` | 2 | Transitive caller traversal depth |
| `--reindex` | false | Force a full AST re-index before tracing |
| `--json` / `--text` | auto | Force output format |

### Fields

`changed_files[]`, `tests[]` (`{test_file, symbol, kind, line, depth, via}`),
`total`.

## git-agent graph diagnose

```
git-agent graph diagnose [symptom] [--file <source>] [--llm] [--top N] [--force] [--json|--text]
```

Trace a regression to the agent action that most likely introduced it. Verifies
the Event Log, derives the Suspect Window between the last passing and first
failing test **Outcome Event**, expands the relevant file set via co-change
`impact`, then ranks the suspect Events deterministically. Each Candidate
carries before/after File Blob Refs so the introducing diff can be
reconstructed. Exits 4 on a chain integrity break unless `--force`.

`--file <source>` seeds the relevant file set and is **effectively required**
for candidates — without it the suspect window has no file set to expand and
diagnose returns no candidates even when the green/red boundary is found.
`[symptom]` (a test name) is optional context. `--llm` re-ranks the top-N
candidates via the `git-agent.diagnose-*` config keys (model, base-url,
api-key, timeout — each falling back to the main provider); it reorders but
never adds candidates. Set them with `git-agent config set diagnose-model
<value>`.

### How Outcome Events are created

diagnose depends on **Outcome Events** in the Event Log marking test pass/fail.
These are created when `capture` records a Bash action whose `tool_response`
contains `go test` output — `capture` parses the response for an exit code and
failure markers (`--- FAIL`, `PASS`, `ok`/`FAIL` lines) and promotes the Event
to `Kind: "outcome"` with the test name and pass/fail. Without Outcome Events
there is no green/red boundary, so diagnose returns no candidates. To use
diagnose: capture the agent's test-run actions (the PostToolUse hook does this
automatically for Bash), then `graph index`, then `graph diagnose`.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--file <source>` | | Seed source file(s) for the relevant set (repeatable). Effectively required for candidates |
| `--llm` | false | Re-rank the top-N candidates with the configured diagnose LLM |
| `--top N` | 5 | Number of candidates passed to the LLM re-rank |
| `--force` | false | Proceed despite an Event Log chain integrity break |
| `--json` / `--text` | auto | Force output format |

## git-agent capture (hidden)

```
git-agent capture --source <src> [--tool <T>] [--instance-id <id>] [--message <m>] [--end-session]
```

Record one agent action (the working-tree delta since the last capture) into the
graph. Designed to run as a Claude Code `PostToolUse` hook — `init --agent-hook`
installs it — and reads `tool_name`/`session_id` from the hook's stdin payload.
Fast (<200ms), no LLM, never blocks the agent on failure. Tooling directories
are excluded from recorded actions.

---

## Defaults and legacy notes

### Hardcoded defaults

When no provider config is found at any level, git-agent falls back to:

| Key | Default |
|-----|---------|
| `base_url` | `https://api.anthropic.com/v1` |
| `model` | `claude-3-5-haiku-20241022` |

### Legacy config migration

- **Project config filename**: `.git-agent/project.yml` is still read for backward compatibility but `.git-agent/config.yml` is the canonical write path. When both exist, `config.yml` takes priority.
- **`hook_type` key**: The old single-string `hook_type` key is automatically migrated to the `hook` array on load. New configs should use `hook`.
