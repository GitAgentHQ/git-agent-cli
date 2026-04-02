# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)

**English** | [简体中文](README.zh-CN.md)

AI-powered Git CLI that analyzes your staged and unstaged changes, splits them into atomic commits, and generates conventional commit messages via LLMs.

## Installation

**Homebrew (macOS/Linux):**

```bash
brew install GitAgentHQ/brew/git-agent
```

**Go install:**

```bash
go install github.com/gitagenthq/git-agent@latest
```

**Pre-built binaries:** download from the [releases page](https://github.com/GitAgentHQ/git-agent-cli/releases).

### Agent skill

Install the git-agent skill to enable AI agents to commit on your behalf:

```bash
npx skills add https://github.com/GitAgentHQ/git-agent-cli --skill use-git-agent
```

## Quick Start

```bash
# Initialize git-agent in your repository
git-agent init

# Stage changes, then generate and create commits
git-agent commit
```

## Commands

### `git-agent init`

Initialize git-agent in the current repository. With no flags, runs the full setup wizard: generates `.gitignore`, generates commit scopes from git history, and writes `.git-agent/config.yml` with scopes and `hook: [conventional]`.

```bash
git-agent init                          # full wizard (gitignore + scopes + conventional hook)
git-agent init --scope                  # generate scopes only
git-agent init --gitignore              # generate .gitignore only
git-agent init --hook conventional      # install conventional commit validator
git-agent init --hook empty             # install empty placeholder hook
git-agent init --hook /path/to/script  # install a custom hook script
git-agent init --force                  # overwrite existing config/hook/.gitignore
git-agent init --max-commits 50        # limit commits analyzed for scope generation
git-agent init --local --scope         # write scopes to .git-agent/config.local.yml
```

| Flag | Description |
|------|-------------|
| `--scope` | Generate scopes via AI |
| `--gitignore` | Generate `.gitignore` via AI |
| `--hook` | Hook to configure: `conventional`, `empty`, or a file path (repeatable) |
| `--force` | Overwrite existing config/.gitignore |
| `--max-commits` | Max commits to analyze for scope generation (default: 200) |
| `--local` | Write config to `.git-agent/config.local.yml` (requires an action flag) |
| `--user` | Write config to `~/.config/git-agent/config.yml` (requires an action flag) |

### `git-agent commit`

Reads staged and unstaged changes, splits them into atomic groups, generates a commit message for each group, and commits them in sequence.

```bash
git-agent commit                              # commit all changes
git-agent commit --dry-run                    # print messages without committing
git-agent commit --no-stage                   # commit already-staged changes only
git-agent commit --amend                      # regenerate and amend the last commit
git-agent commit --intent "fix auth bug"      # provide a context hint to the LLM
git-agent commit --co-author "Name <email>"  # add a co-author trailer
git-agent commit --trailer "Fixes: #123"     # add an arbitrary git trailer
git-agent commit --no-attribution             # omit the default Git Agent trailer
```

### `git-agent config`

Manage git-agent configuration.

```bash
git-agent config show              # display resolved provider config (API key masked)
git-agent config get <key>         # show resolved value and source scope for a key
git-agent config set <key> <value> # write a config value to the appropriate scope
git-agent config set --user api-key sk-xxx   # write to user scope
git-agent config set --project hook empty     # write to project scope
git-agent config set --local max-diff-lines 1000  # write to local scope
```

`config set` and `config get` accept both snake_case and kebab-case keys (e.g., `api-key` and `api_key` are equivalent).

| Scope flag | Config file | Purpose |
|------------|-------------|---------|
| `--user` | `~/.config/git-agent/config.yml` | Provider keys (api_key, base_url, model) |
| `--project` | `.git-agent/config.yml` | Shared, checked into git |
| `--local` | `.git-agent/config.local.yml` | Personal override, gitignored |

When no scope flag is given, provider keys default to `--user` and all others to `--project`.

### `git-agent version`

Print the build version.

## Configuration

### User config (`~/.config/git-agent/config.yml`)

Optional. Points to any OpenAI-compatible endpoint:

```yaml
base_url: https://api.openai.com/v1
api_key: sk-...
model: gpt-4o
```

Examples for other providers:

```yaml
# Cloudflare Workers AI
base_url: https://api.cloudflare.com/client/v4/accounts/YOUR_ACCOUNT_ID/ai/v1
api_key: YOUR_CLOUDFLARE_API_TOKEN
model: "@cf/meta/llama-3.1-8b-instruct"
```

```yaml
# Local Ollama
base_url: http://localhost:11434/v1
model: llama3
```

### Project config (`.git-agent/config.yml`)

Generated by `git-agent init`. Defines commit scopes and hook configuration. Also reads `.git-agent/project.yml` for backward compatibility:

```yaml
scopes:
  - api
  - core
  - auth
  - infra
hook:
  - conventional
```

### Hooks

Configured via `--hook` during `init` or updated later with `git-agent config set hook <value>`:

| Hook | Description |
|------|-------------|
| `conventional` | Validates Conventional Commits format (Go-native) |
| `empty` | Placeholder that always passes |
| `<file path>` | Go validation + shell script at that path |

Custom hooks receive a JSON payload on stdin (`diff`, `commitMessage`, `intent`, `stagedFiles`, `config`) and should exit 0 to allow or non-zero to block. On block, `git-agent` retries up to 3 times before exiting with code 2.

## Flags

### `commit`

| Flag | Description |
|------|-------------|
| `--dry-run` | Print commit messages without committing |
| `--no-stage` | Skip auto-staging; commit only already-staged changes |
| `--amend` | Regenerate and amend the most recent commit (no planning or hooks) |
| `--intent` | Describe the intent of the change |
| `--co-author` | Add a co-author trailer (repeatable) |
| `--trailer` | Add an arbitrary git trailer, format `Key: Value` (repeatable) |
| `--no-attribution` | Omit the default Git Agent co-author trailer |
| `--max-diff-lines` | Maximum diff lines sent to the model (default: 0, no limit) |

### Global

| Flag | Description |
|------|-------------|
| `--api-key` | API key for the AI provider |
| `--model` | Model to use for generation |
| `--base-url` | Base URL for the AI provider |
| `-v, --verbose` | Enable verbose output |
| `--free` | Use only build-time embedded credentials; ignore config file and git config |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error — no changes, API failure, missing config |
| 2 | Hook blocked — pre-commit hook returned non-zero after retries |

## License

[MIT](LICENSE)
