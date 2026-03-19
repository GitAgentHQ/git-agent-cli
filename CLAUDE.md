# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o ga .

# Run all tests
go test ./...

# Run a single package's tests
go test ./application/...
go test ./infrastructure/config/...

# Run a single test by name
go test ./application/... -run TestCommitService_NoStagedChanges

# Run e2e tests (builds the binary internally via TestMain)
go test ./e2e/...

# Run with verbose output
go test ./... -v
```

## Architecture

`ga` follows Clean Architecture with strict inward dependency flow:

```
cmd → application → domain ← infrastructure
```

**`domain/`** — pure Go, zero external imports. Defines value objects and interfaces only: `commit.CommitMessageGenerator`, `diff.DiffFilter`, `diff.StagedDiff`, `hook.HookExecutor`, `hook.HookInput`, `project.Config`.

**`application/`** — two services wired to domain interfaces:
- `CommitService`: AddAll → StagedDiff → (empty check) → Generate → assemble message → hook → Commit
- `InitService`: stubbed; not yet wired to real LLM or file writer

**`infrastructure/`** — implements domain interfaces with real I/O:
- `infrastructure/config/`: three-tier config resolver (CLI flag → `~/.config/ga/config.yml` → default)
- `infrastructure/diff/`: filters lock files and binaries from diffs
- `infrastructure/hook/`: runs `.ga/hooks/pre-commit` as a subprocess, passing `HookInput` as JSON via stdin

**`cmd/`** — cobra wiring only; no business logic. Currently `commit.go` and `init.go` are stubs (RunE returns nil without calling application services). `add.go` is fully wired.

**`pkg/`** — shared utilities: `pkg/errors` (typed exit codes 0/1/2), `pkg/filter` (skip patterns for lock files and binaries).

**`e2e/`** — integration tests that build the `ga` binary via `TestMain` and invoke it as a subprocess. This avoids cobra flag state leaking between tests.

## Key Design Decisions

**Hook protocol**: hooks receive a JSON payload on stdin (`diff`, `commit_message`, `intent`, `staged_files`, `config`). Exit 0 = allow, non-zero = block (exit code 2 from `ga`).

**Config precedence**: CLI flag > `~/.config/ga/config.yml` > zero-config default. The `infrastructure/config/Resolver` handles this; project config (`.ga/project.yml`) provides scopes but not credentials.

**Diff filtering**: `pkg/filter.SkipPatterns` defines lock files and binary extensions excluded before sending to the LLM. `domain/diff/Truncator` caps at `--max-diff-lines` (default 500).

**`cmd` is not yet wired**: `cmd/commit.go` and `cmd/init.go` parse flags but do not call the application services. The application layer (`CommitService`, `InitService`) is complete and tested in isolation. Wiring them to real infrastructure adapters and the cmd layer is the next implementation step.

## Commit Conventions

This repo enforces conventional commits via a pre-tool hook. Commit messages must:
- Title: `type(scope): description` — all lowercase, ≤50 characters
- Valid scopes: `docs`, `plans`, `design`, `cli`
- Body: bullet points (imperative verbs) + closing explanation paragraph
- Body lines ≤72 characters
