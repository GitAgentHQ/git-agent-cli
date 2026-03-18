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
| FR-001 | `ga commit` subcommand via `spf13/cobra` |
| FR-002 | Read staged diff via `git diff --staged` |
| FR-003 | `--intent / -i` flag for LLM context hint |
| FR-004 | Diff filtering (skip lock files, binaries) + line-based truncation |
| FR-005 | Call OpenAI-compatible API, receive JSON: `{commit_message, body, outline}` |
| FR-006 | Assemble full commit message: title + blank line + body + Co-Authored-By footer |
| FR-007 | Execute `git commit -m` non-interactively (headless) |
| FR-008 | stdout = outline only; stderr = errors; exit 0/1/2 |
| FR-009 | `--api-key` flag (fallback: `GA_API_KEY` env) |
| FR-010 | `--model` flag (fallback: `GA_MODEL`, default: `gpt-4o`) |
| FR-011 | `--base-url` flag (fallback: `GA_BASE_URL`) |
| FR-012 | `--co-author` flag (fallback: `GA_CO_AUTHOR`): appended as `Co-Authored-By:` footer |
| FR-013 | `--dry-run` flag: generate message, skip commit |
| FR-014 | `--max-diff-lines` flag (fallback: `GA_MAX_DIFF_LINES`, default: 500) |
| FR-015 | Hook system: `.ga/hooks/pre-commit` executable, JSON via stdin |
| FR-016 | Hook exit 0 = proceed; non-zero = block commit (exit code 2) |
| FR-017 | Validate staged changes exist before LLM call |
| FR-018 | Validate API key presence; clear error if missing |

| FR-019 | `ga init` subcommand: analyze git history + dirs → LLM → write `.ga/config.yml` |
| FR-020 | `ga init` reads up to N recent commits via `git log` to extract scope patterns |
| FR-021 | `ga init` scans top-level directory names as supplementary scope hints |
| FR-022 | `ga init` calls LLM, receives `{scopes, reasoning}`, writes `.ga/config.yml` |
| FR-023 | `ga init` creates `.ga/hooks/pre-commit` as an empty executable placeholder (`exit 0`) |
| FR-024 | `ga init --hook <name>` installs a named built-in hook instead of the empty placeholder |
| FR-025 | Built-in hook `conventional`: validates title format, body, Co-Authored-By footer |
| FR-026 | Built-in hooks are embedded in the `ga` binary (`//go:embed`); no runtime files needed |
| FR-027 | `ga init --force` overwrites existing `.ga/config.yml` and hook; without flag, exits 1 if either exists |
| FR-028 | `ga init --max-commits` flag (default: 200) controls history depth |
| FR-029 | `ga init` stdout = generated `.ga/config.yml` content; stderr = progress/errors |

### Should Have (V1)

| ID | Requirement |
|----|-------------|
| FR-030 | Hook stderr output captured and shown to user |
| FR-031 | `--verbose` flag for debug output to stderr |
| FR-032 | `ga` emits a stderr warning if the generated title exceeds 50 chars or does not match conventional commit format (does NOT block — hooks can block if desired) |

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
| NFR-008 | Hook is optional: if `.ga/hooks/pre-commit` does not exist, proceed without error |
| NFR-010 | `.ga/config.yml` is optional: if absent, no scopes constraint applied |
| NFR-011 | `git config ga.*` read via subprocess (`git config --get`); failure is silent |
| NFR-009 | Hook stderr output on **success** (exit 0) is discarded; only captured on failure |

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

---

## Detailed Design

### Data Flow — `ga init`

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
infrastructure/fs: write .ga/config.yml
     │
     ▼
infrastructure/hooks: resolve hook template
     │   --hook conventional → embed.FS["hooks/conventional.sh"]
     │   (default)           → embed.FS["hooks/empty.sh"]
     ▼
infrastructure/fs: write .ga/hooks/pre-commit (chmod +x)
     │
     ▼
cmd: print config content to stdout, exit 0
     stderr: LLM reasoning + "installed hook: conventional"
```

### Data Flow — `ga commit`

```
ga commit [flags]
     │
     ▼
infrastructure/config: flags → GA_* env → .ga/config.yml → git config ga.* → defaults
     │
     ▼
application/CommitService.Execute()
     │
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
     ├─[6]─► infrastructure/hook: run .ga/hooks/pre-commit
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
│   ├── commit.go                    # `ga commit` — flag parsing, I/O
│   └── init.go                      # `ga init` — flag parsing, I/O
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
│   ├── init_service.go              # InitService: orchestrates ga init flow
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
│   │   ├── config_writer.go         # Write .ga/config.yml
│   │   ├── hook_installer.go        # Write .ga/hooks/pre-commit (chmod +x)
│   │   └── dir_scanner.go           # List top-level directories
│   ├── hooks/
│   │   └── registry.go              # embed.FS loader; maps name → template bytes
│   └── config/
│       ├── resolver.go              # flag → env → project → gitconfig → default
│       ├── project.go               # .ga/config.yml reader
│       └── gitconfig.go             # git config ga.* reader
└── pkg/
    ├── errors/
    │   └── errors.go                # DomainError with exit codes
    └── filter/
        ├── patterns.go              # Lock/binary/generated file patterns
        └── truncator.go             # Line-based truncation
```

### Config Resolution

Four-layer resolution, highest priority first:

```
CLI flag > GA_* env var > .ga/config.yml (project) > git config ga.* (global) > default
```

```
CommitInput.APIKey    = --api-key   → GA_API_KEY   → git config ga.apikey   → error (required)
CommitInput.BaseURL   = --base-url  → GA_BASE_URL  → git config ga.baseurl  → (openai default)
CommitInput.Model     = --model     → GA_MODEL     → "gpt-4o"
CommitInput.Intent    = --intent    → GA_INTENT    → ""
CommitInput.CoAuthor  = --co-author → GA_CO_AUTHOR → ""
CommitInput.MaxLines  = --max-diff-lines → GA_MAX_DIFF_LINES → 500
CommitInput.DryRun    = --dry-run (boolean)
CommitInput.Verbose   = --verbose   → GA_VERBOSE   → false
CommitInput.Scopes    =             →              → .ga/config.yml scopes  → [] (any scope allowed)
```

**Notes**:
- `model` and `co-author` are **not** stored in gitconfig — they are per-commit decisions (flag or env)
- `api-key` and `base-url` may be stored in `~/.gitconfig [ga]` to avoid repeating env vars across machines
- `scopes` only comes from `.ga/config.yml` (team config, version-controlled)

### Project Config: `.ga/config.yml`

Stored in the **repository root**, committed alongside code. Defines team-shared policy:

```yaml
# .ga/config.yml
scopes:
  - api
  - core
  - auth
  - infra
```

When `scopes` is set, ga-cli:
1. Injects the list into the LLM prompt → LLM generates only valid scopes
2. Passes `scopes` in hook JSON payload → hooks can validate without reading files

### Global gitconfig: `~/.gitconfig [ga]`

Personal machine defaults, **not committed**. Only for stable personal preferences:

```ini
# ~/.gitconfig
[ga]
    apikey = sk-...
    baseurl = https://api.openai.com/v1
```

Set via: `git config --global ga.apikey "sk-..."`

`model` and `co-author` are intentionally excluded — they vary per commit.

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

### LLM Response Schema — `ga commit`

```json
{
  "commit_message": "feat(core): implement diff extraction",
  "body": "- Add git diff --staged subprocess\n- Parse file list from diff headers\n\nEnables headless diff extraction without shell interpolation.",
  "outline": "1. Added git diff extraction; 2. Implemented line-based truncation"
}
```

### LLM Response Schema — `ga init`

```json
{
  "scopes": ["api", "core", "auth", "infra"],
  "reasoning": "Extracted from 143 commits: 'api' (38%), 'core' (31%), 'auth' (18%), 'infra' (13%)"
}
```

`scopes` is written to `.ga/config.yml`. `reasoning` is printed to stderr for transparency.

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

1. `ga init` analyzes git history + dirs, generates `.ga/config.yml` with scopes
2. `ga init` on existing config without `--force` exits 1 with clear error
3. `ga commit` generates and applies a conventional commit message from staged diff
4. `ga commit --intent "fix auth bug"` incorporates intent into LLM prompt
5. `ga commit --dry-run` outputs outline without committing
6. `.ga/config.yml` scopes injected into LLM prompt and hook payload when present
7. Pre-commit hook in `.ga/hooks/pre-commit` is executed with correct JSON payload (incl. `config`)
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
