# GitAgent V1 Design — `ga` CLI

**Date**: 2026-03-18
**Status**: Draft
**Scope**: V1 — Agentic CLI core loop

---

## Context

`ga` is an AI-first Git CLI tool that connects Coding Agents (and human developers) to local repositories. It automates commit message generation using an LLM and enforces repository policies via an executable hook system.

**Problem**: Writing semantic commit messages is repetitive, inconsistent, and requires context that a Coding Agent already has. There is no standard bridge between AI agents and Git workflows.

**Solution**: A single Go binary (`ga`) that reads staged diffs, calls an OpenAI-compatible LLM to generate structured commit messages, runs user-defined hook scripts for policy enforcement, and executes the commit — all non-interactively.

---

## Requirements

### Must Have (V1)

| ID | Requirement |
|----|-------------|
| FR-001 | `git agent commit` subcommand via `spf13/cobra` |
| FR-001b| `ga add <pathspecs>` subcommand to stage files via `git add` |
| FR-002 | Read staged diff via `git diff --staged` |
| FR-003 | `--intent / -i` flag for LLM context hint |
| FR-004 | Diff filtering (skip lock files, binaries) + line-based truncation |
| FR-005 | Call OpenAI-compatible API, receive JSON: `{commit_message, body, outline}` |
| FR-006 | Assemble full commit message: title + blank line + body + Co-Authored-By footer |
| FR-007 | Execute `git commit -m` non-interactively (headless) |
| FR-008 | stdout = outline only; stderr = errors; exit 0/1/2 |
| FR-009 | `--api-key` flag (fallback: `~/.config/git-agent/config.yml`) |
| FR-010 | `--model` flag (fallback: `~/.config/git-agent/config.yml`) |
| FR-011 | `--base-url` flag (fallback: `~/.config/git-agent/config.yml`) |
| FR-012 | `--co-author` flag (fallback: `GA_CO_AUTHOR`): appended as `Co-Authored-By:` footer |
| FR-013 | `--dry-run` flag: generate message, skip commit |
| FR-013b| `--all / -a` flag: automatically stage all changes (`git add -A`) before generating commit |
| FR-014 | `--max-diff-lines` flag (fallback: `GA_MAX_DIFF_LINES`, default: 500) |
| FR-015 | Zero-config default: no user credential required; uses project-maintained free endpoint |
| FR-016 | User config home follows XDG: `$XDG_CONFIG_HOME/ga` (fallback: `~/.config/ga`) |
| FR-017 | User config loaded from `~/.config/git-agent/config.yml`: `base_url`, `api_key`, `model` |
| FR-018 | Hook system: `.git-agent/hooks/pre-commit` executable, JSON via stdin |
| FR-019 | Hook exit 0 = proceed; non-zero = block commit (exit code 2) |
| FR-020 | Validate staged changes exist before LLM call |
| FR-021 | Validate API key presence when using custom endpoint; clear error if missing |

| FR-022 | `git agent init` subcommand: analyze git history + dirs → LLM → write `.git-agent/project.yml` |
| FR-023 | `git agent init` reads up to N recent commits via `git log` to extract scope patterns |
| FR-024 | `git agent init` scans top-level directory names as supplementary scope hints |
| FR-025 | `git agent init` calls LLM, receives `{scopes, reasoning}`, writes `.git-agent/project.yml` |
| FR-026 | `git agent init` creates `.git-agent/hooks/pre-commit` as an empty executable placeholder (`exit 0`) |
| FR-027 | `ga init --hook <name>` installs a named built-in hook instead of the empty placeholder |
| FR-028 | Built-in hook `conventional`: validates title format, body, Co-Authored-By footer |
| FR-029 | Built-in hooks are embedded in the `ga` binary (`//go:embed`); no runtime files needed |
| FR-030 | `ga init --force` overwrites existing `.git-agent/project.yml` and hook; without flag, exits 1 if either exists |
| FR-031 | `ga init --max-commits` flag (default: 200) controls history depth |
| FR-032 | `git agent init` stdout = generated `.git-agent/project.yml` content; stderr = progress/errors |

### Should Have (V1)

| ID | Requirement |
|----|-------------|
| FR-032 | Hook stderr output captured and shown to user |
| FR-033 | `--verbose` flag for debug output to stderr |
| FR-034 | `ga` emits a stderr warning if the generated title exceeds 50 chars or does not match conventional commit format (does NOT block — hooks can block if desired) |

### Non-Functional Requirements

| ID | Requirement |
|----|-------------|
| NFR-001 | Binary startup < 20ms (excluding LLM latency) |
| NFR-002 | Binary size < 15MB |
| NFR-003 | Platform: Linux (x86_64, ARM64), macOS (Intel, Apple Silicon) |
| NFR-004 | Git 2.25+ compatibility |
| NFR-005 | >80% unit test coverage on core logic |
| NFR-006 | LLM API call timeout: 60 seconds |
| NFR-007 | Hook execution timeout: 30 seconds |
| NFR-008 | Hook is optional: if `.git-agent/hooks/pre-commit` does not exist, proceed without error |
| NFR-009 | Hook stderr output on **success** (exit 0) is discarded; only captured on failure |
| NFR-010 | `.git-agent/project.yml` is optional: if absent, no scopes constraint applied |
| NFR-011 | `git config ga.*` read via subprocess (`git config --get`); failure is silent |
| NFR-012 | Default uses built-in free endpoint; custom endpoints opt-in via `--base-url`/config |

---

## Rationale

### Why Go?
Single binary, zero runtime dependencies, <20ms startup. Ideal for agent-invoked tooling.

### Why Hook Scripts vs. LLM-Based Validation?
The original design proposed LLM semantic assertion for hook validation. This was replaced with **Claude Code-inspired executable scripts** for three reasons:
1. **Determinism**: Script-based hooks are predictable; LLM validation introduces non-determinism
2. **Composability**: Any language, any logic — teams own their policies
3. **Reliability**: No LLM dependency for policy enforcement; hooks work offline

### Why JSON via stdin for Hooks?
Structured, versioned, language-agnostic. Hooks can be shell scripts, Python, Go binaries, etc. The full commit context is available in one payload.

### Why Exit Code 2 for Hook Blocks?
Distinguishes policy violations from system errors. CI/CD pipelines can handle them differently (e.g., skip on policy block, retry on system error).

### Why stdout = outline only?
The tool is designed for machine consumption. Upstream agents parse stdout directly. All human-readable output goes to stderr.

### Why Zero-Config Default with Universal OpenAI-Compatible Override?
The built-in free endpoint is used by default so first run requires no setup:
1. **Zero onboarding friction**: `git agent commit` works without API key or account setup
2. **Predictable quickstart**: teams can adopt the tool without cloud account dependency
3. **Clean upgrade path**: advanced users set `base_url`/`api_key`/`model` to point to any OpenAI-compatible endpoint (OpenAI, Cloudflare Workers AI, Ollama, LM Studio, Azure OpenAI, etc.)
4. **Operational clarity**: user-owned credentials stay in `~/.config/git-agent/config.yml` only when needed
5. **Maximum compatibility**: any OpenAI-compatible API works — no provider-specific code paths

---

## Detailed Design

### Data Flow — `git agent init`

```
ga init [--force] [--max-commits N] [--hook <name>]
     │
     ▼
infrastructure/git: git log --format="%s" --max-count=N  → commit subjects
     │
     ▼
infrastructure/fs: list top-level directories             → dir hints
     │
     ▼
infrastructure/openai: build prompt (subjects + dirs) → call API
     │              ↓ JSON: {scopes, reasoning}
     ▼
infrastructure/fs: write .git-agent/project.yml
     │
     ▼
infrastructure/hooks: resolve hook template
     │   --hook conventional → embed.FS["hooks/conventional.sh"]
     │   (default)           → embed.FS["hooks/empty.sh"]
     ▼
infrastructure/fs: write .git-agent/hooks/pre-commit (chmod +x)
     │
     ▼
cmd: print config content to stdout, exit 0
     stderr: LLM reasoning + "installed hook: conventional"
```

### Data Flow — `git agent commit`

```
ga commit [flags]
     │
     ▼
infrastructure/config: flags → ~/.config/git-agent/config.yml → .git-agent/project.yml → defaults
     │
     ▼
application/CommitService.Execute()
     │
     ├─[0]─► infrastructure/git: git add -A (if --all is set)
     │              ↓
     ├─[1]─► infrastructure/git: git diff --staged
     │              ↓
     ├─[2]─► domain/diff: filter (lock files, binaries)
     │              ↓
     ├─[3]─► domain/diff: truncate (--max-diff-lines)
     │              ↓
     ├─[4]─► infrastructure/openai: build prompt (inject scopes if set) → call API
     │              ↓ JSON: {commit_message, body, outline}
     ├─[5]─► application: assemble full message (title + body + Co-Authored-By)
     │              ↓
     ├─[6]─► infrastructure/hook: run .git-agent/hooks/pre-commit
     │          JSON stdin: {diff, commit_message, intent, staged_files, config}
     │          exit 0 → proceed | non-zero → exit 2
     │              ↓
     ├─[7]─► infrastructure/git: git commit -m "<full_commit_message>"
     │              ↓
     └─[8]─► cmd: print outline to stdout, exit 0
```

### Project Structure

```
ga-cli/
├── main.go                          # cobra.Execute()
├── go.mod
├── hooks/                           # built-in hook templates (embedded in binary)
│   ├── empty.sh                     # default placeholder (exit 0)
│   └── conventional.sh              # conventional commit validator
├── cmd/
│   ├── root.go                      # root command, global flags
│   ├── commit.go                    # `git agent commit` — flag parsing, I/O
│   ├── add.go                       # `git agent add` — wrapper around git add
│   └── init.go                      # `git agent init` — flag parsing, I/O
├── domain/
│   ├── commit/
│   │   ├── message.go               # CommitMessage value object
│   │   └── generator.go             # CommitMessageGenerator interface
│   ├── diff/
│   │   ├── staged.go                # StagedDiff value object
│   │   ├── filter.go                # DiffFilter interface
│   │   └── truncator.go             # DiffTruncator interface
│   ├── hook/
│   │   ├── executor.go              # HookExecutor interface
│   │   ├── input.go                 # HookInput (JSON schema)
│   │   └── result.go                # HookResult
│   └── project/
│       ├── config.go                # ProjectConfig value object {Scopes}
│       └── generator.go             # ScopeGenerator interface
├── application/
│   ├── commit_service.go            # CommitService: orchestrates commit flow
│   ├── init_service.go              # InitService: orchestrates git agent init flow
│   └── dto/
│       ├── commit_input.go          # CommitInput DTO
│       └── init_input.go            # InitInput DTO {MaxCommits, Force}
├── infrastructure/
│   ├── git/
│   │   ├── client.go                # Git CLI wrapper
│   │   ├── diff_provider.go         # Implements DiffFilter
│   │   ├── committer.go             # git commit executor
│   │   └── log_reader.go            # git log --format="%s" reader
│   ├── openai/
│   │   ├── client.go                # go-openai wrapper
│   │   ├── prompt_builder.go        # Prompt assembly (commit + init)
│   │   └── response_parser.go       # JSON → CommitMessage / ProjectConfig
│   ├── hook/
│   │   ├── executor.go              # .ga/hooks/ runner + path validation
│   │   └── discovery.go             # Hook file existence checks
│   ├── fs/
│   │   ├── config_writer.go         # Write .git-agent/project.yml
│   │   ├── hook_installer.go        # Write .git-agent/hooks/pre-commit (chmod +x)
│   │   └── dir_scanner.go           # List top-level directories
│   ├── hooks/
│   │   └── registry.go              # embed.FS loader; maps name → template bytes
│   └── config/
│       ├── resolver.go              # flag → ~/.config/git-agent/config.yml → default
│       ├── user.go                  # ~/.config/git-agent/config.yml reader
│       ├── project.go               # .git-agent/project.yml reader
│       └── gitconfig.go             # git config ga.* reader
└── pkg/
    ├── errors/
    │   └── errors.go                # DomainError with exit codes
    └── filter/
        ├── patterns.go              # Lock/binary/generated file patterns
        └── truncator.go             # Line-based truncation
```

### Config Resolution

Three-layer resolution, highest priority first:

```
CLI flag > `~/.config/git-agent/config.yml` (user) > `.git-agent/project.yml` (project) > built-in default
```

**Built-in Defaults**:

| Field | Default Value |
|-------|---------------|
| `base_url` | `<project-maintained free endpoint>` |
| `api_key` | `""` (not required for free endpoint) |
| `model` | `<project default free model>` |

Users override these to point to **any** OpenAI-compatible endpoint:

```
CommitInput.APIKey     = --api-key        → config.yml api_key    → "" (free: no key needed)
CommitInput.BaseURL    = --base-url       → config.yml base_url   → <free endpoint>
CommitInput.Model      = --model          → config.yml model      → <free default model>
CommitInput.Intent     = --intent         → ""
CommitInput.CoAuthor   = --co-author      → ""
CommitInput.MaxLines   = --max-diff-lines → 500
CommitInput.DryRun     = --dry-run (boolean)
CommitInput.AutoStage  = --all / -a (boolean)
CommitInput.Verbose    = --verbose        → false
CommitInput.Scopes     =                  → .git-agent/project.yml scopes → [] (any scope allowed)
```

**Notes**:
- Default uses built-in free endpoint for zero-config onboarding
- User credentials/settings live in `~/.config/git-agent/config.yml` (XDG: `$XDG_CONFIG_HOME/ga/config.yml`)
- `scopes` only comes from `.git-agent/project.yml` (team config, version-controlled)
- Any OpenAI-compatible endpoint works: OpenAI, Cloudflare Workers AI, Ollama, LM Studio, Azure OpenAI, etc.

### Project Config: `.git-agent/project.yml`

Stored in the **repository root**, committed alongside code. Defines team-shared policy:

```yaml
# .git-agent/project.yml
scopes:
  - api
  - core
  - auth
  - infra
```

When `scopes` is set, ga-cli:
1. Injects the list into the LLM prompt → LLM generates only valid scopes
2. Passes `scopes` in hook JSON payload → hooks can validate without reading files

### User Config Home: `~/.config/ga/`

Personal machine defaults, **not committed**.

Use `$XDG_CONFIG_HOME/ga` when set; fallback to `~/.config/ga`.

**`~/.config/git-agent/config.yml` (optional):**
```yaml
# Point to any OpenAI-compatible endpoint
base_url: https://api.openai.com/v1
api_key: sk-...
model: gpt-4o
```

**Examples for other providers:**
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

### Hook JSON Schema (with config)

```json
{
  "diff": "<filtered/truncated git diff output>",
  "commit_message": "feat(api): add rate limiting\n\n- Add token bucket middleware\n\nPrevents abuse without blocking legitimate traffic.\n\nCo-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>",
  "intent": "add rate limiting",
  "staged_files": ["src/middleware/ratelimit.go"],
  "config": {
    "scopes": ["api", "core", "auth", "infra"]
  }
}
```

When `CoAuthor` is set, the assembled commit message appends:
```
Co-Authored-By: <CoAuthor value>
```
Example: `GA_CO_AUTHOR="Claude Sonnet 4.6 <noreply@anthropic.com>"`

### Key Interfaces (Domain Layer)

```go
// domain/commit/generator.go
type CommitMessageGenerator interface {
    Generate(ctx context.Context, req GenerateRequest) (*CommitMessage, error)
}

// domain/diff/filter.go
type DiffFilter interface {
    Filter(ctx context.Context, diff *StagedDiff) (*StagedDiff, error)
}

// domain/diff/truncator.go
type DiffTruncator interface {
    Truncate(ctx context.Context, diff *StagedDiff, maxLines int) (*StagedDiff, bool, error)
}

// domain/hook/executor.go
type HookExecutor interface {
    Execute(ctx context.Context, hookName string, input HookInput) (*HookResult, error)
}
```

### Hook JSON Schema

The `commit_message` field contains the **fully assembled** commit message (title + body + footer), so hook scripts can validate the complete format.

```json
{
  "diff": "<filtered/truncated git diff output>",
  "commit_message": "feat(core): implement diff extraction\n\n- Add git diff --staged subprocess\n- Parse file list from diff headers\n\nEnables headless diff extraction without shell interpolation.\n\nCo-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>",
  "intent": "add cache layer",
  "staged_files": ["src/cache.go", "src/service.go"]
}
```

### LLM Response Schema — `git agent commit`

```json
{
  "commit_message": "feat(core): implement diff extraction",
  "body": "- Add git diff --staged subprocess\n- Parse file list from diff headers\n\nEnables headless diff extraction without shell interpolation.",
  "outline": "1. Added git diff extraction; 2. Implemented line-based truncation"
}
```

### LLM Response Schema — `git agent init`

```json
{
  "scopes": ["api", "core", "auth", "infra"],
  "reasoning": "Extracted from 143 commits: 'api' (38%), 'core' (31%), 'auth' (18%), 'infra' (13%)"
}
```

`scopes` is written to `.git-agent/project.yml`. `reasoning` is printed to stderr for transparency.

`commit_message` = title only (type(scope): description, ≤50 chars, lowercase, imperative, no period).
`body` = bullet points (`- Verb ...`) + blank line + explanation paragraph (the "why").
`outline` = stdout output for upstream agents; not written to the commit.

### Commit Message Assembly

ga-cli assembles the full commit message before calling `git commit`:

```
{commit_message}

{body}

Co-Authored-By: {co_author}    ← only if GA_CO_AUTHOR / --co-author is set
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success — commit created (or dry-run complete) |
| 1 | General error — no staged changes, API failure, missing config |
| 2 | Hook blocked — pre-commit hook returned non-zero |

### Dependencies (go.mod)

```
github.com/spf13/cobra v1.8.0
github.com/sashabaranov/go-openai v1.24.0
```

No other external dependencies in V1.

---

## Out of Scope (V1)

- Linear/issue tracker integration (V2)
- Local vector store / semantic history (V3)
- Daemon / autonomous staging (V4)
- Anthropic Claude native SDK (OpenAI-compatible only)
- Interactive mode / TUI
- Color output
- `--amend` support
- Windows native support
- Multiple hook points (only `pre-commit` in V1)
- Post-commit hook

---

## V1 Success Criteria

1. `git agent init` analyzes git history + dirs, generates `.git-agent/project.yml` with scopes
2. `git agent init` on existing config without `--force` exits 1 with clear error
3. `git agent commit` generates and applies a conventional commit message from staged diff
4. `ga commit -a` stages all modifications before generating the commit message
5. `ga add <files>` successfully shells out to `git add`
6. `ga commit --intent "fix auth bug"` incorporates intent into LLM prompt
7. `ga commit --dry-run` outputs outline without committing
6. `.git-agent/project.yml` scopes injected into LLM prompt and hook payload when present
7. Pre-commit hook in `.git-agent/hooks/pre-commit` is executed with correct JSON payload (incl. `config`)
8. Hook exit non-zero → exit code 2, no commit
9. Missing API key → clear error + exit 1
10. stdout = machine-readable output only (outline for commit, config content for init)
11. All error cases handled with descriptive stderr messages

---

## Design Documents

- [BDD Specifications](./bdd-specs.md) — Behavior scenarios and testing strategy
- [Architecture](./architecture.md) — System architecture and component details
- [Best Practices](./best-practices.md) — Security, Go idioms, and code quality guidelines
- [Decisions](./decisions/) — Architecture Decision Records
