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

### Hook retry logic

- 3 retries per commit group
- 2 re-plans maximum if retries are exhausted
- After all retries and re-plans fail: exits with code `2`

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

`--local` without an action flag (`--scope`, `--gitignore`, or `--hook`) returns an error.

### Hook types

| Value | Behavior |
|-------|----------|
| `conventional` | Go-native Conventional Commits 1.0.0 validation |
| `empty` | No-op; always passes |
| `<file path>` | Go validation + shell script at that path |

Shell hooks receive a JSON payload on stdin (`diff`, `commit_message`, `intent`, `staged_files`, `config`). Exit 0 = allow, non-zero = block.

To reconfigure hooks after init: `git-agent config set hook <value>`

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

---

## git-agent version

```
git-agent version
```

Print the build version (injected via ldflags; defaults to `dev` in local builds).
