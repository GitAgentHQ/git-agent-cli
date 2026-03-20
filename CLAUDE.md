# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build (dev, no credentials)
go build -o git-agent .

# Build with free-mode credentials (reads from .env)
bash scripts/build.sh

# Run all tests
go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...

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

`git-agent` follows Clean Architecture with strict inward dependency flow:

```
cmd → application → domain ← infrastructure
```

**`domain/`** — pure Go, zero external imports. Interfaces and value objects only:
- `commit.CommitMessageGenerator` — generates a single commit message from a diff
- `commit.CommitPlanner` / `PlanRequest` / `CommitPlan` / `CommitGroup` — splits changes into atomic commit groups
- `diff.StagedDiff`, `diff.DiffFilter`
- `hook.HookExecutor`, `hook.HookInput`
- `project.Config`

**`application/`** — two services wired to domain interfaces:
- `CommitService`: gets staged+unstaged diffs → auto-scope if no config → plan commits → for each group: unstage-all, stage-group, generate message, hook-retry (3 attempts), re-plan on hook failure (max 2 re-plans) → commit
- `InitService`: delegates scope generation and file writing to `ScopeService`
- `ScopeService`: `Generate(ctx, maxCommits)` calls LLM+git; `MergeAndSave(ctx, path, scopes)` reads existing yaml, deduplicates (case-insensitive), writes merged result

**`infrastructure/`** — implements domain interfaces:
- `infrastructure/config/`: three-tier config resolver (CLI flag → `~/.config/git-agent/config.yml` → default)
- `infrastructure/diff/`: filters lock files and binaries from diffs
- `infrastructure/hook/`: `ShellHookExecutor` runs `.git-agent/hooks/pre-commit` as a subprocess; `CompositeHookExecutor` runs the built-in Go conventional-commit validator first, then delegates to the shell executor
- `infrastructure/git/`: wraps git CLI — `StagedDiff`, `UnstagedDiff`, `StageFiles`, `UnstageAll`, `Commit`, `AddAll`, `CommitSubjects`, `TopLevelDirs`, `ProjectFiles`, `IsGitRepo`
- `infrastructure/openai/`: implements both `CommitMessageGenerator` (`Generate`) and `CommitPlanner` (`Plan`) — the same `*Client` satisfies both interfaces

**`cmd/`** — cobra wiring only; no business logic:
- `init` — `--scope` (bool, AI-generate scopes) + `--hook` (string: `conventional`, `empty`, or file path) + `--force` + `--max-commits`. No flags → defaults to `--scope --hook empty`.
- `commit` — auto-stages all changes, auto-scopes if no project config, splits into atomic commits. Flags: `--dry-run`, `--intent`, `--co-author`, `--api-key`, `--model`, `--base-url`, `--max-diff-lines`.
- `add` command does not exist (removed).

**`pkg/`** — `pkg/errors` (typed exit codes 0/1/2), `pkg/filter` (skip patterns for lock files and binaries).

**`e2e/`** — builds the `git-agent` binary via `TestMain` and invokes it as a subprocess. Avoids cobra flag-state leakage between tests.

## Key Design Decisions

**Hook protocol**: hooks receive a JSON payload on stdin (`diff`, `commit_message`, `intent`, `staged_files`, `config`). Exit 0 = allow, non-zero = block. On block, `git-agent` exits with code 2. The composite executor runs the native Go validator before the shell hook.

**Multi-commit flow**: `CommitService` calls `planner.Plan()` to get a `CommitPlan`, then for each `CommitGroup` it calls `git.UnstageAll()` + `git.StageFiles(group.Files)` before generating and committing. Hook failures after 3 retries trigger a re-plan of the remaining files (capped at 2 re-plans to avoid infinite loops).

**Auto-scope**: if `CommitRequest.Config` is nil or has no scopes, `CommitService` calls `ScopeService.Generate()` and `MergeAndSave()` automatically before planning. Pass `Config: &project.Config{}` (non-nil, empty) to suppress this.

**Config precedence**: CLI flag > `~/.config/git-agent/config.yml` > zero-config default. Project config (`.git-agent/project.yml`) provides scopes; credentials never go there.

**Diff filtering**: `pkg/filter.SkipPatterns` defines lock files and binary extensions excluded before LLM calls. `domain/diff/Truncator` caps at `--max-diff-lines` (default 500).

## Commit Conventions

This repo enforces conventional commits via a pre-tool hook. Commit messages must:
- Title: `type(scope): description` — all lowercase, ≤50 characters
- Valid scopes: `docs`, `plans`, `design`, `cli`
- Body: bullet points (imperative verbs) + closing explanation paragraph
- Body lines ≤72 characters
