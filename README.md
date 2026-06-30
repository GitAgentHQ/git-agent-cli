# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)
[![Latest Release](https://img.shields.io/github/v/release/GitAgentHQ/git-agent-cli)](https://github.com/GitAgentHQ/git-agent-cli/releases)

**English** | [简体中文](README.zh-CN.md)

AI-powered Git CLI that analyzes your staged and unstaged changes, splits them into atomic commits, and generates conventional commit messages via LLMs. It also surfaces co-change relations for agents — which files habitually change together, plus the commits that explain why — all-language, offline, and with no API key.

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
npx skills add https://github.com/GitAgentHQ/git-agent-cli --skill using-git-agent
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
git-agent init --hook /path/to/script   # install a custom hook script
git-agent init --force                  # overwrite existing config/hook/.gitignore
git-agent init --max-commits 50         # limit commits analyzed for scope generation
git-agent init --local --scope          # write scopes to .git-agent/config.local.yml
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

#### `.git-agent/graph.db` is never tracked

The graph database (`.git-agent/graph.db`) is generated at runtime by `commit`,
`related`, and `status` commands. It must never be committed — if it is, every
run re-modifies it and produces a stream of `chore: update graph database file`
commits (the "infinite recreation" loop).

git-agent defends this invariant automatically, with no `init` required:

- **`git-agent init`** writes `.git-agent/graph.db` (+ `*.db-shm`/`*.db-wal`/`*.db-journal` and `.git-agent/config.local.yml`) into the committed `.gitignore`, and runs `git rm --cached` on any already-tracked `graph.db` so the rule can take effect.
- **Runtime defence**: every command that opens the graph DB (`commit`, `related`, `status`) writes the mandatory ignore rules to `.git/info/exclude` (local, untracked, invisible to `git diff`) and untracks `graph.db` if a prior commit tracked it — e.g. a repo cloned from a fork that committed it. This breaks the loop even when `init` has not run.

Verify when in doubt:

```bash
git ls-files .git-agent/graph.db        # must print nothing (untracked)
git check-ignore .git-agent/graph.db    # prints the path, exit 0 (ignored)
```

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
git-agent commit -o json                      # structured result (titles, SHAs, hook outcome)
```

With `-o json`, commit prints a single object: `dry_run`, `commits[]` (each
`{title, message, files, sha, hook_outcome}`), `committed_count`, and
`final_sha`. `hook_outcome` is `passed` or `skipped`. Otherwise output is
human-readable text.

### `git-agent config`

Manage git-agent configuration.

```bash
git-agent config show              # display resolved provider config (API key masked)
git-agent config get <key>         # show resolved value and source scope for a key
git-agent config set <key> <value> # write a config value to the appropriate scope
git-agent config set --user api-key sk-xxx   # write to user scope
git-agent config set --project hook empty     # write to project scope
git-agent config set --local max-diff-lines 1000  # write to local scope
git-agent config set --local max-diff-bytes 524288 # raise the byte cap (e.g., 512 KiB for direct endpoints)
```

`config set` and `config get` accept both snake_case and kebab-case keys (e.g., `api-key` and `api_key` are equivalent).

| Scope flag | Config file | Purpose |
|------------|-------------|---------|
| `--user` | `~/.config/git-agent/config.yml` | Provider keys (api_key, base_url, model) |
| `--project` | `.git-agent/config.yml` | Shared, checked into git |
| `--local` | `.git-agent/config.local.yml` | Personal override, gitignored |

When no scope flag is given, provider keys default to `--user` and all others to `--project`.

### `git-agent completion`

Generate shell completion scripts for git-agent.

```bash
git-agent completion bash         # bash completions
git-agent completion zsh          # zsh completions
git-agent completion fish         # fish completions
git-agent completion powershell   # PowerShell completions
```

To load completions for each session, run once:

```bash
# bash (macOS)
git-agent completion bash > $(brew --prefix)/etc/bash_completion.d/git-agent

# zsh
git-agent completion zsh > "${fpath[1]}/_git-agent"

# fish
git-agent completion fish > ~/.config/fish/completions/git-agent.fish
```

### `git-agent version`

Print the build version.

### `git-agent related`

Show the files that historically change together with the given seeds
(co-change coupling), mined from git history. Seeds are file paths, a
directory, or — with no arguments — your current working-tree changes
("what else usually changes with my edits?"). A file coupled to several seeds
ranks highest.

In JSON output, each related file carries a `commits` array of
`{sha, subject, ts}` — the commits that link it to a seed, i.e. the evidence
for *why* the two files are coupled. Use `--tests` to keep only related test
files, a fast "which tests should I run after this change?".

Language-agnostic (it reads git history, not source parsing), offline (no LLM,
no API key), and auto-indexed on first run.

```bash
git-agent related                                        # "what else changes with my edits?"
git-agent related application/commit_service.go          # co-change from a specific file
git-agent related src/                                   # co-change from a directory
git-agent related application/commit_service.go --tests  # related test files only
git-agent related application/commit_service.go -o json  # adds the linking `commits` array
```

| Flag | Default | Description |
|------|---------|-------------|
| `--depth` | 1 | Transitive co-change depth |
| `--top` | 20 | Max results |
| `--min-count` | 3 | Minimum co-change count to include |
| `--tests` | false | Keep only related test files |
| `--reindex` | false | Force a full re-index before querying |
| `-o`, `--output` | auto | Output format: `auto`, `json`, `text` (JSON when piped, text on a TTY) |

### `git-agent status`

Report code-graph index health and row counts: commits, files, authors,
co-change pairs, the last indexed commit, and database size. Offline (no LLM,
no API key).

```bash
git-agent status              # index health + row counts
git-agent status -o json      # structured output
git-agent init --graph        # one-shot cold build (commit-history co-change)
```

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
| `--max-diff-lines` | Maximum diff lines sent to the model (default: 0, no line limit; a byte cap always applies) |
| `--max-diff-bytes` | Maximum diff bytes sent to the model (default: 0, falls back to the built-in ~384 KiB cap; pass a positive value to override) |
| `-o`, `--output` | Output format: `text` (default), `json`, or `auto` (JSON when piped) |

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
| 3 | Retired/unused (no longer emitted) |
| 4 | Retired/unused — formerly Event Log chain integrity; the Event Log subsystem has been removed (no longer emitted) |

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history.

## License

[MIT](LICENSE)
