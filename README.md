# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)
[![Latest Release](https://img.shields.io/github/v/release/GitAgentHQ/git-agent-cli)](https://github.com/GitAgentHQ/git-agent-cli/releases)

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
git-agent init --agent-hook             # install capture hook for Claude Code PostToolUse
git-agent init --force                  # overwrite existing config/hook/.gitignore
git-agent init --max-commits 50         # limit commits analyzed for scope generation
git-agent init --local --scope          # write scopes to .git-agent/config.local.yml
```

| Flag | Description |
|------|-------------|
| `--scope` | Generate scopes via AI |
| `--gitignore` | Generate `.gitignore` via AI |
| `--hook` | Hook to configure: `conventional`, `empty`, or a file path (repeatable) |
| `--agent-hook` | Install a `PostToolUse` capture hook for Claude Code |
| `--force` | Overwrite existing config/.gitignore |
| `--max-commits` | Max commits to analyze for scope generation (default: 200) |
| `--local` | Write config to `.git-agent/config.local.yml` (requires an action flag) |
| `--user` | Write config to `~/.config/git-agent/config.yml` (requires an action flag) |

#### `.git-agent/graph.db` is never tracked

The graph database (`.git-agent/graph.db`) is generated at runtime by `commit`,
`capture`, `timeline`, and `graph` commands. It must never be committed — if it
is, every run re-modifies it and produces a stream of `chore: update graph
database file` commits (the "infinite recreation" loop).

git-agent defends this invariant automatically, with no `init` required:

- **`git-agent init`** writes `.git-agent/graph.db` (+ `*.db-shm`/`*.db-wal`/`*.db-journal` and `.git-agent/config.local.yml`) into the committed `.gitignore`, and runs `git rm --cached` on any already-tracked `graph.db` so the rule can take effect.
- **Runtime defence**: every command that opens the graph DB (`capture`, `timeline`, `graph *`) writes the mandatory ignore rules to `.git/info/exclude` (local, untracked, invisible to `git diff`) and untracks `graph.db` if a prior commit tracked it — e.g. a repo cloned from a fork that committed it. This breaks the loop even when `init` has not run.

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

### `git-agent graph`

Query the deterministic code graph: the AST index and the commit-history
co-change index. The AST index parses git-tracked Go files in your repo and
records functions, methods, structs/interfaces, **struct fields**, type aliases,
imports, calls, and field-read `references` edges; co-change is derived from git
history. Offline (no LLM, no API key). For forensic queries over the agent Event
Log, see [`git-agent audit`](#git-agent-audit).

**Symbol syntax** for `callers` / `callees` / `symbol` — accepts a bare name, a
receiver-qualified `Type.Method`, or a fully-qualified `file::Type.Method`:

```bash
git-agent graph callers Flag                  # all callers of any Flag method
git-agent graph callers decoder.alias         # narrow to one receiver type
git-agent graph callers "decode.go::decoder.alias"  # fully-qualified
git-agent graph callers HideHelpCommand       # struct field reads surface here
git-agent graph symbol Commit.Run             # signature + one-hop trails
git-agent graph affected command.go           # tests exercising the file's symbols
git-agent graph search --kind method Connect  # FTS5 symbol search
```

```bash
git-agent graph status        # index health + row counts
git-agent init --graph        # one-shot full graph build (co-change + Event-Log + AST)
git-agent graph impact        # co-change coupling for files (see below)
git-agent graph callers       # symbols that call or reference a symbol (blast radius)
git-agent graph callees       # symbols called or referenced by a symbol
git-agent graph symbol        # a symbol's location, signature, caller/callee trail
git-agent graph search        # FTS5 symbol search
git-agent graph affected      # test files exercising the given files' symbols
git-agent graph external-refs # call/field sites reaching into external packages
```

**External packages are not indexed.** The index only parses files in your
repo, so symbols from imported packages (e.g. `github.com/spf13/pflag`) are
never AST nodes. `callers`/`symbol` report this explicitly instead of failing
with a bare "not found"; `graph external-refs` lists every call/field-read
site that reaches into an external package:

```bash
git-agent graph callers pflag.Lookup
# Error: symbol "pflag.Lookup" is exported by external package
# "github.com/spf13/pflag", which is not indexed; run
# `git-agent graph external-refs` to list call sites into it

git-agent graph external-refs            # all external-package reference sites
git-agent graph external-refs -o json
```

> **Build note:** AST commands (`callers`, `callees`, `symbol`, `search`,
> `affected`, `index`) require a tree-sitter build
> (`CGO_ENABLED=1 go build`). Release binaries are compiled with
> `CGO_ENABLED=0` and stub these out; `external-refs` reads only unresolved
> refs and works in either build. After upgrading the binary on a repo with an
> existing `.git-agent/graph.db`, run `git-agent init --graph` (full rebuild of
> all three layers) — or `git-agent graph index --reindex` (AST-only) — to pick
> up struct-field nodes and receiver-resolved call edges on the old DB.

#### Does `graph` help a model develop features?

An A/B re-test (2026-06-27) ran a capable agent on three real Go repos
(`spf13/cobra`, `go-yaml/yaml`, `urfave/cli`) — each feature implemented twice,
once without `graph` (grep/Read only) and once with `graph` forensic commands.
All six arms built and tested green; the graph did not flip any fail→pass. It
**did** deliver measurable non-trivial value the no-graph arm lacked:

- **Field disambiguation (cli):** `graph search Hide` returned empty (no such
  field) while `graph search Hidden` returned the field node + its 19 readers
  via `graph callers Hidden`. A bare `grep Hide` matches three separate fields
  (`Hidden`, `HideHelp`, `HideHelpCommand`); the graph prevented a wrong-field
  accessor.
- **Receiver disambiguation (yaml):** `graph symbol alias` showed both
  `parser.alias` and `decoder.alias` source + signatures in one call,
  revealing that `decoder.alias` only dereferences already-parsed alias nodes
  — the real anchor-capture site is `parser.anchor`. Both arms landed on the
  right site, but the graph made it one structured call vs. a 30-line noise grep.
- **Test fidelity to the spec (yaml):** the with-graph arm's test covered
  cross-`Decode` persistence (4 sub-cases) the no-graph arm's single-case test
  did not — even though both implementations were identical.
- **Cross-file consumer safety (cobra):** `graph callers mergePersistentFlags`
  surfaced cross-file consumers in `flag_groups.go` and `completions.go`,
  confirming a new read-only accessor wouldn't disturb them.

The graph's value is investigation depth, test/invariant fidelity, and
cross-file safety — not enabling the impossible. It shines most on unfamiliar
codebases where grep noise is high and receiver/field disambiguation matters.

### `git-agent graph impact`

Show the files that historically change together with the given seeds
(co-change coupling). Seeds are file paths, a directory, or — with no
arguments — your current working-tree changes. A file coupled to several seeds
ranks highest. The first run auto-indexes git history; queries are offline (no
LLM, no API key).

For symbol-level structural blast radius — which symbols call or reference a
function — use `git-agent graph callers <symbol> --depth N` instead.

```bash
git-agent graph impact                                     # "what else changes with my edits?"
git-agent graph impact application/commit_service.go       # co-change from a specific file
git-agent graph impact src/                                # co-change from a directory
git-agent graph impact application/commit_service.go -o json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--depth` | 1 | Transitive co-change depth |
| `--top` | 20 | Max results |
| `--min-count` | 3 | Minimum co-change count to include |
| `--reindex` | false | Force a full re-index before querying |
| `-o`, `--output` | auto | Output format: `auto`, `json`, `text` (JSON when piped, text on a TTY) |

### `git-agent audit`

Query and audit the agent Event Log — the append-only, hash-chained record of
every captured agent and human action. All queries are read-only and offline
(no LLM, no API key).

```bash
git-agent audit timeline      # agent/human action history (start here)
git-agent audit diagnose      # trace a regression to its introducing action
git-agent audit provenance    # rename-aware change history for a file
git-agent audit verify        # Event Log chain integrity (exits 4 on a break)
```

#### `git-agent audit timeline`

Show recent agent and human action history grouped into sessions, with the tool and files for each action. Populated by `git-agent capture`. Offline.

```bash
git-agent audit timeline                        # all recorded actions
git-agent audit timeline --since 2h             # last 2 hours
git-agent audit timeline --file src/auth.go     # actions touching a file
git-agent audit timeline --source claude-code   # filter by source
git-agent audit timeline -o json                # JSON output
```

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | | Time window: `2h`, `7d`, or RFC 3339 timestamp |
| `--source` | | Filter by action source (e.g. `claude-code`, `human`) |
| `--file` | | Filter by file path |
| `--top` | 50 | Max sessions to display |
| `-o`, `--output` | auto | Output format: `auto`, `json`, `text` |

#### `git-agent audit diagnose`

Trace a regression to the agent action that most likely introduced it: derives the suspect window between the last passing and first failing test outcome, expands the file set via co-change, and ranks the suspect actions. `--file <source>` seeds the relevant set (effectively required for candidates); `[symptom]` is optional context; `--llm` re-ranks the top candidates via the configured diagnose LLM. Exits 4 on an Event Log chain break unless `--force`.

#### `git-agent audit provenance`

Reconstruct a file's full, rename-aware change history from the Event Log: every captured change plus out-of-band edits, folding in pre-rename identities. Out-of-band rows are flagged. Usage: `git-agent audit provenance <file>`.

#### `git-agent audit verify`

Walk the hash-chained Event Log and verify it has not been tampered with (recomputes each event's hash, checks chain linkage and sequence continuity). Exits 4 on any integrity break.

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
| 3 | Graph not indexed — a `graph` read ran before the index was built (run `git-agent init --graph`, or let the next `commit` build it) |
| 4 | Event Log chain integrity broken (`audit verify` / `audit diagnose`) |

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history.

## License

[MIT](LICENSE)
