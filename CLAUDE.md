# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build / test / install (preferred — use Makefile targets)
make build        # dev build, no credentials
make test         # all tests with -count=1 (no cache)
make install      # install to $GOPATH/bin

# Build with embedded credentials (reads from .env)
bash scripts/build.sh

# Run a single package
go test ./application/...

# Run a single test by name
go test ./application/... -run TestCommitService_NoStagedChanges

# Format all Go files (auto-runs on edit via hook; no golangci-lint configured)
gofmt -w ./...
```

**e2e tests**: `TestMain` builds the `git-agent` binary once, then all tests invoke it as a subprocess. After any source change, re-run `go test ./e2e/...` — the stale binary will not reflect changes.

## Architecture

Clean Architecture with strict inward dependency flow:

```
cmd → application → domain ← infrastructure
```

- **`domain/`** — pure Go, zero external imports; interfaces and value objects only
- **`application/`** — orchestration services (`CommitService`, `InitService`, `ScopeService`, `GitignoreService`)
- **`infrastructure/`** — adapters: git CLI wrappers, OpenAI client, config resolver, Toptal API client
- **`cmd/`** — Cobra wiring only, no business logic
- **`pkg/errors/`** — typed exit codes (0 = success, 1 = general error, 2 = hook blocked commit, 3 and 4 = retired/unused)
- **`e2e/`** — full binary tests via subprocess

## Key Design Decisions

**Hook dispatch**: driven by `hook` in `.git-agent/config.yml` (legacy `hook_type` in `project.yml` is auto-migrated). `""` or `"empty"` → skip validation entirely. `"conventional"` → Go-native `ValidateConventional` only (not the `conventional.sh` shell script). Any other value → Go validation first, then treat as a file path and run via shell. Shell hooks receive a JSON payload on stdin; exit 0 = allow, non-zero = block. After 3 retries, `git-agent` exits with code 2.

**Multi-commit flow**: for each planned `CommitGroup`, `CommitService` calls `git.UnstageAll()` then `git.StageFiles(group.Files)` before generating and committing. Hook failures after 3 retries trigger a re-plan of remaining files (capped at 2 re-plans). If any group title lacks a scope, scopes are refreshed and the plan is regenerated once.

**Amend flow**: `--amend` calls `LastCommitDiff()`, generates a new message, and calls `AmendCommit()`. No planning or hook execution.

**Auto-scope**: if `CommitRequest.Config` is nil or has no scopes, `CommitService` calls `ScopeService.Generate()` and `MergeAndSave()` automatically before planning. Pass `Config: &project.Config{}` (non-nil, empty) to suppress this.

**Config precedence**: CLI flag > `~/.config/git-agent/config.yml` > zero-config default. Project config (`.git-agent/config.yml`) provides scopes, hooks, and behavior flags — credentials never go there. Local overrides in `.git-agent/config.local.yml` take precedence over project config.

**Trailer handling**: trailers are assembled in `cmd/commit.go` and appended via `git interpret-trailers` before each `git.Commit()`. On hook retry, `previousMessage = preTrailer` (title + body without trailers) so trailers never enter LLM context.

## Command Surface Conventions

The CLI is a Cobra tree. Every command lives in exactly one of three namespaces; do not add top-level commands outside these.

### Namespaces

- **Action** (top-level): `init`, `commit`. These mutate the repo or the graph. (The hidden `capture` hook target and the `audit` forensic tree were removed when the agent Event Log subsystem was cut — the graph is now commit-history co-change only.)
- **Meta** (top-level): `config`, `version`, `completion`. Configuration and tooling, not repo mutation.
- **Reads** (top-level): `related` and `status`. `related <files...>` is the co-change query — the files that habitually change with the given files, enriched with the commits that link them (subject + sha + date); language-agnostic (git history, not parsing), offline, no API key. `status` reports index health and row counts. A new co-change read goes at the top level here.

**There is no query namespace parent.** The graph is a single data source (git-history co-change); there is no append-only Event Log and no separate forensic trust model. Reads are top-level `related`/`status`.

### Registration

Each command registers itself exactly once in its own `init()` via `rootCmd.AddCommand(xCmd)`. Never register a command twice, and never prefix a child's `Use` with the parent name — Cobra composes the path from `Use` verbatim.

### Output format

Every read command takes a single `-o, --output {auto,json,text}` flag, registered via `addOutputFlag` (local on `related`/`status`/`commit`/`version`). `auto` (the query default) emits **JSON when stdout is piped, text on a TTY**; `commit`/`version` default to `text` so piping a human-facing action does not silently switch it to JSON. Resolve the format with `outputFormat(cmd)` (wraps `pkg/output.Decide`), encode with `pkg/output.EncodeJSON`, and emit error envelopes with `pkg/output.EncodeError`. Wrap a read command's `RunE` in `jsonAwareRunE` so failures render as `{"error":{"code","message"}}` on stderr in JSON mode. Do not hand-roll `--json`/`--text` or `json.NewEncoder` in a new command. (`commit`'s `stderrIsTerminal` is a separate stderr concern for progress gating.)

### Flag policy

Prefer config keys over per-command flags. A value belongs on the command line only if it is (a) a behavioral toggle (`--force`, `--reindex`, `--amend`), (b) a per-invocation override of query shape (`--depth`, `--top`, `--kind`, `--file`), or (c) a path / free-form argument. Provider credentials, models, base URLs, and timeouts are config keys (`git-agent config set <key> <value>`), never flags. When sinking a flag to config, the key must already exist in `infrastructure/config/keys.go` `KeyRegistry` and be read by the resolver.

### Short descriptions

- A parent `Short` describes the group, not a single action. A parent without `RunE` must not claim to "Show" anything — it prints help. Use a group verb: `Manage …`.
- A child `Short` is verb-leading and ≤ ~60 characters.
- `Short` must match what the command does; update it when the command moves namespace.

### Hidden commands

There are currently no hidden commands (the `capture` hook target was removed with the Event Log). Graph building is automatic (via `commit` / `init --graph` and read-path auto-sync), so there are no manual index/sync commands.

## Commit Conventions

Enforced via pre-tool hook. Commit messages must:
- Title: `type(scope): description` — all lowercase, ≤50 characters
- Valid scopes: `docs`, `plans`, `design`, `cli`
- Body: bullet points (imperative verbs) + closing explanation paragraph
- Body lines ≤72 characters
