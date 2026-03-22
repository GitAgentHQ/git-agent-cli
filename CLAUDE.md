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

**`application/`** — services wired to domain interfaces:
- `CommitService`: gets staged+unstaged diffs → auto-scope if no config → plan commits → for each group: unstage-all, stage-group, generate message, hook-retry (3 attempts), re-plan on hook failure (max 2 re-plans) → commit
- `InitService`: delegates scope generation and file writing to `ScopeService`
- `ScopeService`: `Generate(ctx, maxCommits)` calls LLM+git (`CommitLog` provides subject+files per commit); `MergeAndSave(ctx, path, scopes)` reads existing yaml, deduplicates (case-insensitive), writes merged result
- `GitignoreService`: calls `TechDetector` (LLM) to identify technologies, fetches content from `ContentGenerator` (Toptal), merges into `.gitignore` preserving a `### custom rules ###` section below the auto-generated block
- `AddService`: thin wrapper around `git.Add`; no CLI exposure

**`infrastructure/`** — implements domain interfaces:
- `infrastructure/config/`: three-tier config resolver (CLI flag → `~/.config/git-agent/config.yml` → default); `gitconfig.go` reads `git-agent.*` keys from local git config via `ReadGitConfig`/`ReadGitConfigBool`
- `infrastructure/diff/`: filters lock files and binaries from diffs
- `infrastructure/hook/`: `ShellHookExecutor` runs a file path as a subprocess; `CompositeHookExecutor` dispatches by `hookType`: `""` or `"empty"` → pass immediately, `"conventional"` → Go-native `ValidateConventional` only, any other value → Go validation then `ShellHookExecutor`
- `infrastructure/git/`: wraps git CLI — `StagedDiff`, `UnstagedDiff`, `StageFiles`, `UnstageAll`, `Commit`, `AmendCommit`, `AddAll`, `FormatTrailers`, `LastCommitDiff`, `CommitSubjects`, `CommitLog`, `TopLevelDirs`, `ProjectFiles`, `IsGitRepo`, `RepoRoot`, `GitDir`
- `infrastructure/openai/`: implements `CommitMessageGenerator` (`Generate`), `CommitPlanner` (`Plan`), and `TechDetector` (`DetectTechnologies`) — the same `*Client` satisfies all three interfaces
- `infrastructure/gitignore/`: `ToptalClient` implements `ContentGenerator`; fetches `.gitignore` content from the Toptal API for a list of technology names

**`domain/`** additions beyond interfaces noted above:
- `domain/commit/validator.go`: `ValidateConventional` enforces Conventional Commits 1.0.0 + project rules (≤50-char title, lowercase description, bullet points, ≤72-char body lines, explanation paragraph, `Co-Authored-By` format); returns `*ValidationResult` with typed `SeverityError`/`SeverityWarning` issues
- `domain/gitignore/`: `TechDetector` and `ContentGenerator` interfaces

**`hooks/`** (package at repo root) — embedded shell templates via `//go:embed`:
- `empty.sh`: no-op hook (reference/test only)
- `conventional.sh`: standalone conventional-commit checker (reference/test only; the built-in `"conventional"` hookType uses Go-native validation, not this script)

**`cmd/`** — cobra wiring only; no business logic:
- `init` — flags: `--scope`, `--hook` (accepts `conventional`, `empty`, or a file path; repeatable), `--gitignore`, `--force`, `--max-commits` (default 200), `--local` (write to `.git-agent/config.local.yml`). No flags → full wizard (scope + gitignore + conventional hook). `--local` requires at least one action flag.
- `commit` — auto-stages all changes, auto-scopes if no project config, splits into atomic commits. Flags: `--dry-run`, `--intent`, `--co-author`, `--trailer` (format `"Key: Value"`), `--no-attribution` (omit default Git Agent trailer; `--no-git-agent` is a deprecated alias), `--no-stage` (skip auto-staging), `--amend` (regenerate last commit), `--max-diff-lines`. `--amend` and `--no-stage` are mutually exclusive (enforced by Cobra). Output per commit: `git commit` stdout (hash + file stats) followed by the explanation paragraph extracted from the LLM-generated body via `extractExplanation`.
- `config show` — display resolved provider config (`api_key` masked, `model`, `base_url`). Respects global `--api-key`/`--model`/`--base-url` overrides.
- `config get <key>` — show resolved value and source scope. Accepts both snake_case and kebab-case keys.
- `config set <key> <value>` — write a config key to the specified scope (`--user`, `--project`, `--local`). Accepts both snake_case and kebab-case keys (e.g., `api-key` → `api_key`).
- `version` — print the build version.

**`pkg/`** — `pkg/errors` (typed exit codes 0/1/2), `pkg/filter` (skip patterns for lock files and binaries).

**`e2e/`** — builds the `git-agent` binary via `TestMain` and invokes it as a subprocess. Avoids cobra flag-state leakage between tests.

## Key Design Decisions

**Hook dispatch**: `CommitRequest.Config.HookType` (from `project.yml` `hook_type`) drives hook execution. `""` or `"empty"` → skip validation entirely. `"conventional"` → Go-native `ValidateConventional` only, no shell script. Any other value → treat as file path: Go validation first, then `ShellHookExecutor`. Shell hooks receive a JSON payload on stdin (`diff`, `commit_message`, `intent`, `staged_files`, `config`); exit 0 = allow, non-zero = block. On block after retries, `git-agent` exits with code 2.

**Multi-commit flow**: `CommitService` calls `planner.Plan()` to get a `CommitPlan`, then for each `CommitGroup` it calls `git.UnstageAll()` + `git.StageFiles(group.Files)` before generating and committing. Hook failures after 3 retries trigger a re-plan of the remaining files (capped at 2 re-plans to avoid infinite loops). If any planned group title lacks a scope, scopes are refreshed and the plan is regenerated once.

**Amend flow**: `--amend` calls `git.LastCommitDiff()`, generates a new message, and calls `git.AmendCommit()`. No planning or hook execution occurs.

**Trailer handling**: Trailers are assembled in `cmd/commit.go` then passed to `application.CommitRequest.Trailers`; appended to each assembled message via `git interpret-trailers` before `git.Commit()`. Order: (1) `--co-author` values (skipped entirely if `NoModelCoAuthor` is set in provider or project config); (2) arbitrary `--trailer` pairs; (3) default `Co-Authored-By: Git Agent <noreply@git-agent.dev>` (skipped if `--no-attribution` / `--no-git-agent` flag is set, or if `NoGitAgentCoAuthor` is set in provider or project config). `preTrailer` (title + body before `FormatTrailers`) is kept separately; on hook retry `previousMessage = preTrailer` so trailers never enter LLM context. The display output calls `extractExplanation(c.Body)` where `c.Body = msg.Body` (raw LLM output, no trailers), so trailers do not appear in terminal output under normal conditions.

**Auto-scope**: if `CommitRequest.Config` is nil or has no scopes, `CommitService` calls `ScopeService.Generate()` and `MergeAndSave()` automatically before planning. Pass `Config: &project.Config{}` (non-nil, empty) to suppress this.

**Config precedence**: CLI flag > `~/.config/git-agent/config.yml` > zero-config default. Project config (`.git-agent/project.yml`) provides scopes; credentials never go there.

**Diff filtering**: `pkg/filter.SkipPatterns` defines lock files and binary extensions excluded before LLM calls. `domain/diff/Truncator` caps at `--max-diff-lines` (default 500).

## Commit Conventions

This repo enforces conventional commits via a pre-tool hook. Commit messages must:
- Title: `type(scope): description` — all lowercase, ≤50 characters
- Valid scopes: `docs`, `plans`, `design`, `cli`
- Body: bullet points (imperative verbs) + closing explanation paragraph
- Body lines ≤72 characters
