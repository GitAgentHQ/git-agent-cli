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
| FR-005 | Call OpenAI-compatible API, receive JSON: `{commit_message, outline}` |
| FR-006 | Execute `git commit -m` non-interactively (headless) |
| FR-007 | stdout = outline only; stderr = errors; exit 0/1/2 |
| FR-008 | `--api-key` flag (fallback: `GA_API_KEY` env) |
| FR-009 | `--model` flag (fallback: `GA_MODEL`, default: `gpt-4o`) |
| FR-010 | `--base-url` flag (fallback: `GA_BASE_URL`) |
| FR-011 | `--dry-run` flag: generate message, skip commit |
| FR-012 | `--max-diff-lines` flag (fallback: `GA_MAX_DIFF_LINES`, default: 500) |
| FR-013 | Hook system: `.aig/hooks/pre-commit` executable, JSON via stdin |
| FR-014 | Hook exit 0 = proceed; non-zero = block commit (exit code 2) |
| FR-015 | Validate staged changes exist before LLM call |
| FR-016 | Validate API key presence; clear error if missing |

### Should Have (V1)

| ID | Requirement |
|----|-------------|
| FR-017 | Hook stderr output captured and shown to user |
| FR-018 | `--verbose` flag for debug output to stderr |
| FR-019 | `ga` emits a stderr warning if the generated message does not match conventional commit format (does NOT block — hooks can block if desired) |

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
| NFR-008 | Hook is optional: if `.aig/hooks/pre-commit` does not exist, proceed without error |
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

### Data Flow

```
ga commit [flags]
     │
     ▼
infrastructure/config: resolve flags → GA_* env → defaults
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
     ├─[4]─► infrastructure/openai: build prompt → call API
     │              ↓ JSON: {commit_message, outline}
     ├─[5]─► infrastructure/hook: run .aig/hooks/pre-commit
     │          JSON stdin: {diff, commit_message, intent, staged_files}
     │          exit 0 → proceed | non-zero → exit 2
     │              ↓
     ├─[6]─► infrastructure/git: git commit -m "<commit_message>"
     │              ↓
     └─[7]─► cmd: print outline to stdout, exit 0
```

### Project Structure

```
ga-cli/
├── main.go                          # cobra.Execute()
├── go.mod
├── cmd/
│   ├── root.go                      # root command, global flags
│   └── commit.go                    # `ga commit` — flag parsing, I/O
├── domain/
│   ├── commit/
│   │   ├── message.go               # CommitMessage value object
│   │   └── generator.go             # CommitMessageGenerator interface
│   ├── diff/
│   │   ├── staged.go                # StagedDiff value object
│   │   ├── filter.go                # DiffFilter interface
│   │   └── truncator.go             # DiffTruncator interface
│   └── hook/
│       ├── executor.go              # HookExecutor interface
│       ├── input.go                 # HookInput (JSON schema)
│       └── result.go                # HookResult
├── application/
│   ├── commit_service.go            # CommitService: orchestrates full flow
│   └── dto/
│       └── commit_input.go          # CommitInput DTO
├── infrastructure/
│   ├── git/
│   │   ├── client.go                # Git CLI wrapper
│   │   ├── diff_provider.go         # Implements DiffFilter
│   │   └── committer.go             # git commit executor
│   ├── openai/
│   │   ├── client.go                # go-openai wrapper
│   │   ├── prompt_builder.go        # Prompt assembly
│   │   └── response_parser.go       # JSON → CommitMessage
│   ├── hook/
│   │   ├── executor.go              # .aig/hooks/ runner + path validation
│   │   └── discovery.go             # Hook file existence checks
│   └── config/
│       └── resolver.go              # flag → env → default resolution
└── pkg/
    ├── errors/
    │   └── errors.go                # DomainError with exit codes
    └── filter/
        ├── patterns.go              # Lock/binary/generated file patterns
        └── truncator.go             # Line-based truncation
```

### Config Resolution

```
CommitInput.APIKey   = --api-key flag → GA_API_KEY env → error (required)
CommitInput.Model    = --model flag   → GA_MODEL env   → "gpt-4o"
CommitInput.BaseURL  = --base-url flag → GA_BASE_URL env → (openai default)
CommitInput.Intent   = --intent flag  → GA_INTENT env  → ""
CommitInput.MaxLines = --max-diff-lines → GA_MAX_DIFF_LINES → 500
CommitInput.DryRun   = --dry-run flag (boolean)
CommitInput.Verbose  = --verbose flag → GA_VERBOSE env → false
```

API key is the only **required** config. All others have defaults or are optional.

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

```json
{
  "diff": "<filtered/truncated git diff output>",
  "commit_message": "feat(core): implement diff extraction",
  "intent": "add cache layer",
  "staged_files": ["src/cache.go", "src/service.go"]
}
```

### LLM Response Schema

```json
{
  "commit_message": "feat(core): implement diff extraction logic",
  "outline": "1. Added git diff extraction; 2. Implemented line-based truncation"
}
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
- Config files (flags + env vars only)
- Interactive mode / TUI
- Color output
- `--amend` support
- Windows native support

---

## V1 Success Criteria

1. `ga commit` generates and applies a conventional commit message from staged diff
2. `ga commit --intent "fix auth bug"` incorporates intent into LLM prompt
3. `ga commit --dry-run` outputs outline without committing
4. Pre-commit hook in `.aig/hooks/pre-commit` is executed with correct JSON payload
5. Hook exit non-zero → exit code 2, no commit
6. Missing API key → clear error + exit 1
7. stdout = outline only (parseable by upstream agents)
8. All error cases handled with descriptive stderr messages

---

## Design Documents

- [BDD Specifications](./bdd-specs.md) — Behavior scenarios and testing strategy
- [Architecture](./architecture.md) — System architecture and component details
- [Best Practices](./best-practices.md) — Security, Go idioms, and code quality guidelines
- [Decisions](./decisions/) — Architecture Decision Records
