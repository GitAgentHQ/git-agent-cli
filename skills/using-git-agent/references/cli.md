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

## Exit codes

git-agent uses a typed exit-code taxonomy across all commands:

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (no API key, git error, no changes, etc.) |
| `2` | Commit blocked by a hook after retries |
| `3` | Retired / unused (co-change reads auto-index on first run) |
| `4` | Retired / unused — formerly "Event Log chain integrity"; the Event Log subsystem has been removed |

---

## Output format

Every read command takes a single `-o, --output` flag:

```
-o, --output {auto,json,text}
```

- `auto` (the default for `related` and `status` reads): JSON when stdout is piped, text on a TTY.
- `json`: force machine-readable JSON.
- `text`: force human-readable text.

`commit` and `version` also accept `-o` but **default to `text`** — pass `-o json` explicitly for machine output.

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
| `-o`, `--output` | string | `text` | Output format: `text`, `json`, or `auto` (JSON when piped) |

`--amend` and `--no-stage` are mutually exclusive (enforced at parse time by Cobra).

### JSON output (`-o json`)

With `-o json`, commit prints a single structured object instead of progress text:

```json
{
  "dry_run": false,
  "commits": [
    {
      "title": "feat(cli): add output flag",
      "message": "feat(cli): add output flag\n\n- ...",
      "files": ["cmd/commit.go"],
      "sha": "a1b2c3d",
      "hook_outcome": "passed"
    }
  ],
  "committed_count": 1,
  "final_sha": "a1b2c3d"
}
```

- Each `commits[]` entry carries `title`, `message` (full message without trailers), `files`, `sha`, and `hook_outcome`.
- `hook_outcome` is `passed` (a validating hook ran and accepted the commit) or `skipped` (no validating hook configured).
- Top level: `dry_run`, `committed_count`, `final_sha` (the `sha` of the last commit).
- On `--dry-run`, `committed_count` is `0` and `sha`/`final_sha` are empty.

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
2. Generates `.gitignore` via AI — always includes `.git-agent/graph.db` and `*.db-shm`/`*.db-wal`/`*.db-journal`, and untracks `.git-agent/graph.db` if it is already in the index (prevents the runtime graph database from being committed)
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
git-agent version [-o <format>]
```

Print the build version (injected via ldflags; defaults to `dev` in local builds). Defaults to text output; pass `-o json` for a machine-readable object.

---

## git-agent related

```
git-agent related [path...]
```

Show the files that historically change together with the given seeds
(co-change coupling), mined from git history. Seeds are one or more files, a
directory (expands to its tracked files), or — with **no arguments** — the
current working-tree changes ("given what I've edited, what else usually
moves?"). Co-change neighbours are aggregated across all seeds, so a file
coupled to several seeds ranks above one coupled to a single seed. The query is
**language-agnostic** (it reads commit history, not source code), offline (no
LLM / API key), and the first run auto-indexes git history. Tooling directories
(`.git-agent/`, `.claude/`) are never used as seeds.

In JSON, each related file also carries a `commits` array — the actual commits
that linked it to a seed (`{sha, subject, ts}`) — so you can read *why* two
files are coupled, not just that they are.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--depth N` | 1 | Transitive co-change depth; depth > 1 entries are marked `[indirect, depth N]` |
| `--top N` | 20 | Max results |
| `--min-count N` | 3 | Minimum co-change count to include (index floor is 2; values below 2 cannot surface more) |
| `--reindex` | false | Force a full re-index before querying |
| `--tests` | false | Keep only related test files — "which tests should I run for this change?" |
| `-o, --output` | auto | Output format: `auto`, `json`, or `text` (JSON when piped, text on a TTY) |

### Output

Text shows `path  strength%  (N co-changes)`, with `[M/T seeds: ...]` when more
than one seed. JSON fields per entry: `path`, `coupling_count`,
`coupling_strength`, `score` (sum of strengths over matched seeds — the rank
key), `seed_matches`, `related_to` (which seeds), `depth`, and `commits`
(`[{sha, subject, ts}]` — the commits that link this file to a seed). Top-level:
`targets`, `co_changed`, `total_found`, `query_ms`.

```bash
git-agent related application/commit_service.go cmd/commit.go -o json
git-agent related internal/auth                                  # a whole module
git-agent related                                                # seeds = my current edits
git-agent related --tests                                        # which tests to run for my edits
```

## git-agent status

```
git-agent status [-o <format>]
```

Snapshot of index health: whether the index exists, the last indexed commit,
and row counts. Read-only; auto-syncs projections before reading. The first
`related` run auto-indexes git history, so
`commit_count`/`file_count`/`author_count`/`co_changed_count` stay 0 until then.

### Fields

`exists`, `last_indexed_commit` (omitempty — absent until git history is
indexed by the first `related`), `commit_count`, `file_count`,
`author_count`, `co_changed_count`, `session_count`, `action_count`,
`db_size_bytes` (SQLite page_count × page_size).

## git-agent init --graph

```
git-agent init --graph
```

One-shot cold start that builds the code graph in a single pass — no LLM
needed: `EnsureIndex` with Force reads git history and recomputes `co_changed`
(the data `related` reads).

This is opt-in: the default `init` wizard does NOT build the graph. The graph
builds automatically instead — the first `git-agent commit` bootstraps and
maintains it (via `graph_autobuild`), and every read syncs the index before
reading. Run `init --graph` only for an explicit full cold start.

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
