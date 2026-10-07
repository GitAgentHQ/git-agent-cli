# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)
[![Latest Release](https://img.shields.io/github/v/release/GitAgentHQ/git-agent-cli)](https://github.com/GitAgentHQ/git-agent-cli/releases)

**English** | [简体中文](README.zh-CN.md)

AI-powered Git CLI that analyzes your staged and unstaged changes, splits them into atomic commits, and generates conventional commit messages via LLMs.

## Installation

**Homebrew (macOS/Linux):**

```bash
brew install gitagenthq/brew/git-agent
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

The skill is a discovery stub — the full usage guide ships in the binary:
`git-agent skills get core` prints the main guide (triggers, workflows, flags,
exit codes) and `git-agent skills get cli` prints the complete command
reference, always matching the installed version.

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

### `git-agent` (bare command)

The default agent-oriented entry point. It inspects the repository, performs
normal autonomous checks, splits changes into atomic groups, generates commit
messages, and commits them in sequence.

```bash
git-agent --intent "fix auth bug"           # default agent workflow
```

### `git-agent commit`

Explicit commit subcommand for cases where the autonomous root workflow is not
wanted. It reads staged and unstaged changes, splits them into atomic groups,
generates a commit message for each group, and commits them in sequence.

```bash
git-agent commit                              # explicit commit path
git-agent commit --dry-run                    # print messages without committing
git-agent commit --no-stage                   # commit already-staged changes only
git-agent commit --amend                      # regenerate and amend the last commit
git-agent commit --intent "fix auth bug"      # provide a context hint to the LLM
git-agent commit --co-author "Name <email>"  # add a co-author trailer
git-agent commit --trailer "Fixes: #123"     # add an arbitrary git trailer
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
git-agent config set language auto             # follow --intent language, else English
git-agent config set --local language Japanese # override language locally
git-agent config set --local max-diff-lines 1000  # write to local scope
git-agent config set --local max-diff-bytes 524288 # raise the byte cap (e.g., 512 KiB for direct endpoints)
git-agent config set --local max-plan-files 300     # raise the planner file-list cap before it collapses to directory summaries
```

`config set` and `config get` accept both snake_case and kebab-case keys (e.g., `api-key` and `api_key` are equivalent).

| Scope flag | Config file | Purpose |
|------------|-------------|---------|
| `--user` | `~/.config/git-agent/config.yml` | Provider keys and Cloudflare AI Gateway ID |
| `--project` | `.git-agent/config.yml` | Shared, checked into git |
| `--local` | `.git-agent/config.local.yml` | Personal override, gitignored |

When no scope flag is given, provider keys default to `--user` and all others to `--project`.

Generated commit messages use `language: auto` by default: the title description,
bullets, and explanation follow the language of a clear `--intent` directive,
falling back to English when no clear language is present. Set an explicit
language such as `git-agent config set language Japanese` to use it consistently.
The local config overrides project config, which overrides user config. Conventional
commit type and scope tokens keep their standard syntax; only natural-language
text is translated.

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

## Configuration

### User config (`~/.config/git-agent/config.yml`)

Optional. Points to any OpenAI-compatible endpoint:

```yaml
base_url: https://api.openai.com/v1
api_key: sk-...
model: gpt-4o
```

### Free shared gateway (zero config)

Official release binaries point at a free shared gateway by default, so **no
configuration is required** — just run `git-agent commit`. Your requests are
routed through a Cloudflare Worker that holds the upstream credential
server-side (never in the binary) and rate-limits anonymous free usage.

To opt out and use your own endpoint, set `base_url` (and optionally `api_key`
/ `model`) — any user config overrides the built-in gateway URL.

Examples for other providers:

```yaml
# Bring your own key — Cloudflare AI Gateway + Workers AI
base_url: https://api.cloudflare.com/client/v4/accounts/YOUR_ACCOUNT_ID/ai/v1
api_key: YOUR_CLOUDFLARE_API_TOKEN
model: "@cf/zai-org/glm-4.7-flash"
cloudflare_ai_gateway_id: YOUR_GATEWAY_ID # use "default" for the default gateway
```

When you bring your own key, git-agent routes requests through that endpoint
directly; for Cloudflare, `cloudflare_ai_gateway_id` opts into the gateway,
disables prompt/response payload storage while retaining metadata, and leaves
retries to the CLI so retry layers cannot multiply.

```yaml
# Local Ollama
base_url: http://localhost:11434/v1
model: llama3
```

### Optional System One layer (Jev)

git-agent can hand the decisions it can answer to [TypeSafe](https://typesafe.ai)'s
System One model, Jev, and keep the text model for wording. The split follows
what each model is for:

| Decision | Owner | Why |
| --- | --- | --- |
| Which scope covers which top-level directory | Jev, when the mode is `on` | Measured correct on this repository's own scopes |
| Which technologies a project uses | **The text model** | Measured: exact on a Go repository, one of four on a React repository |
| The scope in a commit title | Jev, above a 0.7 confidence floor | Measured in agreement with the recorded titles more often than the baseline |
| How changed files group into commits | **The text model** | Measured worse: it over-splits one change into one commit per directory |
| The commit type | **The text model** | Measured worse than the baseline, and its confidence carries no signal |
| Which lever a rejected commit needs | **The text model** | Not yet measured |
| Commit message wording, scope descriptions | The text model | Jev writes no text |

A judgment runs on a structured summary: changed paths, line counts, and the
symbols the diff touched. The diff body never travels to the second provider,
which keeps the cost down and the source code inside the repository's existing
provider boundary.

### Modes

| Mode | Behavior |
| --- | --- |
| `shadow` (default with a key) | Every judgment runs and is recorded; the text model still decides |
| `on` | A confident judgment decides |
| `off` | No judgment runs |

Shadow mode is the default because a judgment has to be compared with the
behavior it replaces before it replaces it. It costs both providers: one Jev
request per seam plus the model's own call.

```bash
git-agent config set jev_api_key YOUR_TYPESAFE_KEY   # or export TYPESAFE_API_KEY
git-agent config set jev_mode on                     # shadow is the default
git-agent config set jev_min_confidence 0.7         # default 0.7
git-agent config set jev_max_calls 4                # per-run call budget
git-agent config set jev_model jev-latest            # optional, this is the default
```

Run `git-agent -v commit` to read one line per judgment: what it decided, what the
model decided, whether they agree, the latency, and the state size.

Every judgment falls back to the text model when the service is unreachable,
when the answer is less confident than its floor, or when the per-run call budget
is spent. A judgment never blocks a commit.

A seam that misses its floor twice in one run stops being called for the rest of
that run, so a seam that cannot reach its floor costs two calls instead of one
per commit. Shadow mode is exempt: there a miss is the expected outcome, because
measuring is the purpose of the mode.

### Measuring a seam

The evaluation harness replays a repository's own history through a judgment and
reports agreement with the recorded commit titles. It needs a key and never runs
in the normal suite:

```bash
TYPESAFE_API_KEY=... JEV_EVAL_REPO=. go test -tags jevlive -v ./infrastructure/eval/
```

The report separates the two halves of a commit prefix, because they are gated
separately, and it lists the confidence each disagreement happened at. A floor
only means something when the wrong answers arrive below it.

Scoring a judgment against the human title is not enough. A judgment is only
worth adopting when it beats the behavior it replaces, so the comparison that
matters runs the generative provider and the judgment over the same commits:

```bash
TYPESAFE_API_KEY=... AB_BASE_URL=... AB_MODEL=... \
  go test -tags jevlive -v -run TestLiveAgainstBaseline ./infrastructure/eval/
```

Run over three repositories, that comparison is what moved the grouping, type and
technology seams back to the text model: the judgment lost the type in 21 of 32
samples against 26 of 32 for the baseline, it split changes the baseline kept
whole, and it found one of four technologies in a React repository. All three
still run in shadow mode, so they stay measured and nothing replaces a working
decision. Only the per-directory scope judgment remains adoptable, because it
decided a repository's own scopes correctly and costs one request.

If the measurements never move, delete the layer. It is additive: no seam is
required by any other, and removing `infrastructure/typesafe` plus its wiring
leaves the CLI exactly as it was.

## Project config (`.git-agent/config.yml`)

Generated by `git-agent init`. Defines commit scopes and hook configuration. Also reads `.git-agent/project.yml` for backward compatibility:

```yaml
scopes:
  - api
  - core
  - auth
  - infra
hook:
  - conventional
language: auto # or an explicit language such as Japanese
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
| `--max-diff-lines` | Maximum diff lines sent to the model (default: 0, no line limit; a byte cap always applies) |
| `--max-diff-bytes` | Maximum diff bytes sent to the model (default: 0, falls back to the built-in ~384 KiB cap; pass a positive value to override) |
| `--max-plan-files` | Maximum file paths listed individually in the planner prompt before collapsing to directory summaries (default: 0, falls back to the built-in cap of 150) |
| `-o`, `--output` | Output format: `text` (default), `json`, or `auto` (JSON when piped) |

### Global

| Flag | Description |
|------|-------------|
| `--api-key` | API key for the AI provider |
| `--model` | Model to use for generation |
| `--base-url` | Base URL for the AI provider |
| `-v, --verbose` | Enable verbose output |

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
