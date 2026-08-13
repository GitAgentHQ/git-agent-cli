# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.12.0] - 2026-08-13

### Added
- Show the resolved session model in `config` output, so agents can see which model will be attributed as the co-author (cli)

### Changed
- Clarify co-author model precedence and the zero-config session model in the skill guides (skills)

### Fixed
- Remove the agent-session environment-variable fallback (`PI_MODEL`, `CLAUDE_CODE_MODEL`, `CODEX_MODEL`, `MODEL`, ...) from model resolution: the generation model now comes only from the `--model` flag, `git config --local git-agent.model`, or the user config file. A session-injected model could previously silently override the configured endpoint model — e.g. swapping a fast config model for a slow reasoning model routed through a local proxy, stalling every commit in length-retry loops (cli)
- Attribute commit authorship to the session model rather than the inference model (#19) (infra)
- Exclude the generic model from co-author attribution when a session model is present (infra)
- Restore the co-author model fallback when the session model cannot be mapped to a known provider (cli)

## [0.11.1] - 2026-08-11

### Fixed
- Fix the release workflow matrix race: five parallel build jobs each tried to create and finalize the same GitHub release via `softprops/action-gh-release`, failing with `already_exists` errors that left the v0.11.0 release missing half its assets. Builds now upload artifacts in parallel and a single publish job creates the release and uploads all assets (ci)

## [0.11.0] - 2026-08-11

### Added
- Add an autonomous agent workflow to the root command (#16) (cli)
- Improve model resolution and variant parsing (cli)
- Infer the model co-author from the active model (cli)
- Add model co-author inference (domain)
- Add `pi_model` and model environment fallbacks (infra)
- Add `init` commands to regenerate scopes and `.gitignore` (cli)
- Auto-initialize scopes and `.gitignore` when none are configured (app)

### Docs
- Update skill guides for model inference (skills)
- Document scope and gitignore regeneration

## [0.10.1] - 2026-08-07

### Fixed
- Fix the release workflow reading an unset `GATEWAY_URL` secret: point it at the existing `WORKER_URL` secret so tag-triggered releases build with the embedded gateway URL instead of failing at the Build step (ci)

### Docs
- Document the `--free` flag in the CLI reference and skill docs: it forces routing via the shared gateway, clarifies configuration precedence for the override, and updates error guidance to suggest `--free` for auth issues (docs)

## [0.10.0] - 2026-08-07

### Added
- Add a free shared gateway as the zero-config default: official release binaries embed the gateway URL (never a credential), so `git-agent commit` works out of the box with no API key or config. The Worker holds the upstream token server-side and rate-limits anonymous free usage.
- Add a `--free` routing flag that forces routing via the shared gateway: it clears any custom `api_key`/`model` and pins `base_url` to the embedded gateway URL, overriding custom credentials and endpoints while keeping auxiliary settings (cli)
- Add direct Cloudflare AI Gateway routing through `cloudflare_ai_gateway_id` for bring-your-own-key setups; Gateway requests retain usage metadata without storing prompt or response payloads, and keep retries owned by the CLI
- Add a seed-exclusive hubness penalty to `related` ranking that demotes high-fanout "hub" files (changelogs, vendored dirs) so focused module partners rank above unrelated noise, plus a synthetic backtest runner to validate recall (infra)

### Changed
- Relax the provider-config gate: with no `api_key`, git-agent routes to the free shared gateway (base_url only, model pinned server-side); supplying an `api_key` switches to direct mode and requires `base_url` + `model`
- Build and release credential-free binaries; the only build-time default is the gateway URL, never a token
- `config show -o json` reports mode `"free"` only when `base_url` equals the embedded shared-gateway URL with no `api_key`; custom endpoints and no-provider dev builds are labeled `"configured"` (cli)

### Fixed
- Threshold the hubness penalty at >1 non-seed partner so a single cohesive module partner is not demoted below an unrelated weaker file (infra)
- Resolve a bare hook filename in `.git-agent/config.yml` against the repo root so `hook: [my-hook.sh]` executes instead of failing with "executable file not found in $PATH" (infra)
- Make `release.yml` fail fast when the `GATEWAY_URL` secret is unset, preventing a gateway-less release binary from shipping silently with green CI (ci)

### Removed
- Remove the old shared `--free`/embedded-credential flow that shipped the proxy's `CLIENT_TOKEN` in public binaries — the free gateway now holds its own credential server-side instead
- Remove the `init --graph` flag: `related` auto-indexes git history on first run and `--reindex` forces a full rebuild (cli)

## [0.9.0] - 2026-08-04

### Added
- Add `skills` command with `list` and `get` subcommands: usage guides are embedded in the binary at build time and served by the CLI, so documentation always matches the installed version (cli)
- Add a skill documentation registry embedding core and cli guides; the `using-git-agent` skill stub is now a discovery portal that delegates to the CLI for the full guide (skills)

## [0.8.0] - 2026-08-02

### Added
- Cap per-commit file lists at 300 (infra)
- Clamp min-count to index floor (app)
- Add token limit preflight guard (infra)
- Show partial graph index status (cli)
- Add second example for co-author flag (cli)
- Lower default min-count to 2

### Changed
- Expand built-in `DefaultModelCoAuthorDomains` so `require_model_co_author: true` accepts common AI providers without extra `model_co_author_domains` config: `anthropic.com`, `openai.com`, `google.com`, `x.ai`, `zhipuai.cn`, `qwen.ai`, `deepseek.com`, `moonshot.ai`. Custom / lesser-known providers still use `model_co_author_domains`.
- `max_input_tokens` project config key raises the preflight input-size ceiling (default 1M tokens) for endpoints with larger context windows; the LLM input guard now has an override instead of a fixed constant.
- Simplify output flag registration (cli)
- Simplify output flag signature (cli)
- Update min-count default documentation
- Document consolidated testing history

### Fixed
- Restore the `build-check` CI workflow (gates push/PR to `main`/`develop` with a `CGO_ENABLED=0` build, cross-compile, `gofmt` check, and the full test suite). It was removed in the prior `ci: remove pr-gated build check` commit, which left only the tag-time `release.yml` — a cgo-only dependency or broken test could then merge undetected and only surface at release time.
- `status` now reports `Graph: not indexed` (with db size and a build hint) on a repo whose graph has no indexed commits, instead of `Graph: indexed (last commit (none))`. The `Long` help now says "whether the index is built" rather than "whether the index exists", matching actual behavior.
- `status` `db size` now rolls over to GiB/TiB for large databases instead of printing a large MiB number.
- Stale `outputFormat` doc comment no longer claims `-o` is "inherited from a parent command" — it is registered locally on each command.
- Update mincount fallback to 2 (infra)

## [0.7.0] - 2026-07-07

### Added
- `related` co-change query: ranks the files that habitually change with the seed(s) — a file coupled to several seeds ranks highest — and attaches the linking commits (`{sha, subject, ts}`) that prove each coupling. With no arguments it uses the current working-tree changes. `--tests` keeps only the related test files (which tests to run after a change).
- `status` top-level read: graph index health and row counts.
- `commit -o json` structured output: a single object with `dry_run`, `commits[]` (each `{title, message, files, sha, hook_outcome}`), `committed_count`, and `final_sha`. Read commands emit a uniform `{"error":{"code","message"}}` envelope on stderr on failure in JSON mode.
- `init --graph` flag: one-shot cold start that builds the commit-history co-change index without an LLM. The default `init` wizard does not build the graph (opt-in) — the first `commit` does, via `graph_autobuild`.
- `graph_autobuild` config key (project/local): set `false` to stop `commit` from building and maintaining the co-change graph.
- `--max-plan-files` flag / `max_plan_files` config key: caps how many individual file paths the planner prompt lists before collapsing them into directory-level summaries (e.g. `vendor/lib/ (842 files)`), default 150. Fixes `commit` hanging or timing out when a changeset touches thousands of files (e.g. untracking a vendored dependency directory newly covered by `.gitignore`) — the file list, not just the diff content, could overwhelm the planner. Collapsing targets the largest offending directories first, leaving small groups listed individually, and the real file paths are recovered after the LLM responds so staging/committing is unaffected.
- Atomic rename commits: a git-detected file rename (staged or a worktree move paired by content hash) has its old and new path forced into the same commit group, both after initial planning and after every re-plan, so a move is never split into an orphaned delete in one commit and add in another. The co-change graph also records renames, and `related` resolves rename aliases to the file's current path.

### Changed (BREAKING)
- **Co-change-only graph — all AST/static-analysis machinery removed.** The graph is now built purely from git co-change history: language-agnostic, offline, no API key, no cgo. Deleted the AST extractors, symbol index, reference resolver, and every AST-backed read (~3,000 lines).
- **Command-surface refactor — clean break, no compatibility aliases.** The whole `graph` namespace and the `impact` family are removed and replaced:
  - `git-agent impact <files…>` → top-level **`git-agent related <files…>`** — the co-change query, enriched with the commits that prove each coupling (subject + sha + date); `--tests` narrows to related test files.
  - `git-agent graph status` → top-level **`git-agent status`** — index health + row counts.
  - Removed with no replacement: `graph index`, `graph sync`, `graph query`, `graph node`, `graph callers`, `graph callees`, `graph external-refs`, `graph affected`, and `graph impact`. Graph building is automatic (via `commit` / `init --graph` and read-path auto-sync), so there are no manual index/sync commands.
- **Unified output flag.** The per-command `--json` / `--text` pair is replaced by a single `-o, --output {auto,json,text}` flag on every read command (`auto` = JSON when stdout is piped, text on a TTY). `commit` and `version` also accept `-o` but default to `text`.
- **Agent Event Log subsystem removed.** The append-only, hash-chained action log and all of its forensic machinery are gone — the graph is now a single data source (git commit-history co-change) and the LLM serves only `commit`/`init --scope`. Deleted the `audit` command tree (`timeline`/`diagnose`/`provenance`/`verify`), the hidden `capture` command, the `--agent-hook` PostToolUse installer, the CQRS projection/replay path, the out-of-band reconcile service, the SHA-256 hash chain, the `diagnose` LLM re-ranker, and the `redact` package (~4,100 lines).
- **Exit codes 3 and 4 retired.** `3` was "graph not indexed" (co-change reads auto-index on first run, so the condition no longer exists); `4` was "Event Log chain integrity" (no Event Log to verify). Live exit codes are now `0` success, `1` general error, `2` hook blocked commit.
- **Schema bumped to v4.** The `events`/`event_files`/`sessions`/`actions`/`action_modifies`/`action_produces` tables are dropped on open (idempotent), so existing v3 databases shed them without a full rebuild. Opening a pre-refactor database also drops the retired AST tables idempotently, preserving co-change data without a full rebuild.
- **`git-agent status`** no longer reports `sessions`/`actions` counts (those tables no longer exist); it shows commits, files, authors, co-change pairs, last indexed commit, and db size.
- **`git-agent init --graph`** builds the commit-history co-change index only (the L3 Event-Log projection step is gone). The `--agent-hook` flag is removed.
- **`git-agent commit`** no longer syncs the Event Log or links captured actions to the commit (there is no Event Log); the co-change hint provider and post-commit autobuild are unchanged.

## [0.6.1] - 2026-06-28

### Added
- Add `is-tracked` and `untrack-file` git methods to detect and untrack files (infra)

### Changed
- Simplify and remove reasoning model (pkg)
- Harden graph.db ignore and rename skill (skills)
- Remove outdated skill documentation (skills)

### Fixed
- Centralize graph.db path and harden commands (infra)
- Harden graph.db untrack on init and runtime (cli)
- Enforce gitignore for the graph database and rename skill (app)
- Untrack graph.db after the gitignore rule is applied (cli)

## [0.6.0] - 2026-06-27

### Changed
- Migrate AST extraction from tree-sitter to the standard library `go/ast` package, removing the cgo dependency entirely so graph commands (`impact`, `index`, `sync`, `callers`, `callees`, `node`, `query`, `affected`) now work in `CGO_ENABLED=0` release builds

### Added
- Port struct-field indexing, field-read reference edges, and receiver-var call rewrite from the tree-sitter extractor to the `go/ast` extractor so cross-package symbol edges continue to link correctly
- Add build-check CI workflow (`.github/workflows/build-check.yml`) to guard `CGO_ENABLED=0` builds

### Fixed
- Prevent duplicate edges on calls in graph processing to ensure accurate impact analysis

## [0.5.2] - 2026-06-27

### Fixed
- Resolve AST receivers, index fields, and external-package graph references so cross-package symbol edges link correctly
- Surface external-package graph references instead of dropping them during indexing
- Stop tracking the runtime `.git-agent/graph.db` artifact so it no longer pollutes commits

## [0.5.1] - 2026-06-26

### Fixed
- Unblock cgo-free release builds after the v0.5.0 tag shipped zero binaries: the release workflow builds with `CGO_ENABLED=0`, but the tree-sitter-based extractor is cgo-only and failed to compile
- Stub the tree-sitter extractor behind a `//go:build cgo` build tag so `CGO_ENABLED=0` release binaries build cleanly (infra)
- Surface a clear "AST extraction unavailable" runtime error for AST-dependent graph commands (`impact`, `index`, `sync`, `callers`, `callees`, `node`, `query`, `affected`) in release binaries, with a pointer to rebuild with `CGO_ENABLED=1`
- A follow-up v0.6.0 will port the extractor to the standard library `go/ast` package to remove the cgo dependency entirely

## [0.5.0] - 2026-06-26

### Added
- Code knowledge graph backed by a SQLite repository, capturing actions, AST nodes, edges, and session state
- AST-based impact analysis with multi-seed queries, deterministic traversal ordering, and resolution metadata on edges
- Incremental AST indexing and sync (`graph index`, `graph sync`) with per-file produce tracking and schema versioning
- Graph query and audit subcommands under `graph`: `status`, `verify`, `index`, `sync`, `impact`, `timeline`, `diagnose`, `provenance`, `callers`, `callees`, `node`, `query`, `affected`
- `--json` / `--text` output flags with TTY auto-detection, routed through the new `pkg/output` helper
- LLM re-ranker wired into `graph diagnose` for forensic ranking of impact results
- Capture event log redesign for audit and forensics, including redaction and an event sequence/repo abstraction
- Automatic agent capture via the Claude Code PostToolUse hook (`capture --source claude-code`)
- Co-change index with exponential decay coupling and a lowered co-change floor
- Embedding and FTS5 search support in the graph repository
- `ErrNothingToCommit` sentinel error and graceful empty-commit handling
- Empty-scope handling for fresh repositories with retry for unscoped planning

### Changed
- Migrated the code graph engine from KuzuDB to SQLite
- Centralized database connection logic and unified AST index `Ensure` methods
- Simplified symbol impact, indexing, linking, and language extraction logic
- Decoupled capture handling and made action batch creation atomic with baseline updates
- Normalized impact command path inputs and limited impact output size
- Disabled `core.quotepath` and resolved git paths from the repository root

### Fixed
- Prevented self-pollution in code-graph capture
- Validated database schema version early to avoid corrupt-state reads
- Handled nil seeds and edge duplicates in AST resolution
- Prevented path corruption and added UUID identifiers
- Normalized commit-empty index errors and repository execution errors
- Resolved warnings display in diagnosis output

### Docs
- Added graph subcommand documentation and git-agent graph skill docs
- Added capture event log redesign design and plan
- Updated CLI features, release history, and impact documentation

### Chore
- Removed stale learning state and the obsolete graph rebuild command
- Untracked SQLite database files and updated gitignore

## [0.4.0] - 2026-05-29

### Added
- `--max-diff-bytes` flag and `max_diff_bytes` config key (project/local scopes) to cap the byte size of the diff sent to the LLM
- Always-on byte cap (default ~384 KiB) so vendored or minified diffs no longer exceed the provider's request-body limit
- Fallback planner with timeout and retry logic to handle large-diff edge cases
- Planner timeout configuration with automatic fallback behavior

### Changed
- Commit progress output messaging improved for clarity
- Planning progress messages clarified and standardized
- Commit status output simplified for consistency
- LLM heartbeat messages silenced in CLI output
- Diff truncation now uses byte-cap UTF-8 safe truncation strategy

### Fixed
- Large-diff stuck symptoms fully resolved with fallback and timeout handling
- Auto-scope and scope-refresh no longer wipe `hook`, co-author policy, or the new byte cap from the in-memory config when generating scopes mid-commit
- UTF-8-safe byte truncation: a trailing partial multi-byte rune is dropped on a rune boundary; mid-string invalid bytes are preserved so a hang previously caused by whole-string validation is gone
- Line calculation and heartbeat sync issues corrected
- Commit rejection output message improved
- Plan config fallback and numstat truncation issues resolved
- TTY output gating and planner fallback fixed
- Timeout retries capped to one attempt
- Signal routing and timeout configuration wired correctly
- Config preservation when bound to UTF-8 truncation

### Docs
- Completion command documentation added
- Large-diff stuck remediation design and plan documented

## [0.3.0] - 2026-05-11

### Added
- `--version` flag to display build version
- Scope whitelist validation to enforce allowed scopes in commits
- Pre-flight co-author validation to enforce model-specific attribution
- Model-specific co-author trailer enforcement (Anthropic, OpenAI, Google)
- Support for zero-commit repositories via filesystem walk
- `AllChangedFiles` function to list staged, unstaged, and untracked files
- CHANGELOG.md with Keep a Changelog format for version documentation

### Changed
- Replaced `AddAll` with `AllChangedFiles` to preserve staging intent
- Scope generation instructions updated to prioritize description-based matching
- Hook executor integrates co-author validation for all hooks

### Fixed
- Empty diff edge cases handled correctly in commit service
- Verbose test output properly reflects unstaged files sequence

### Docs
- Model co-author requirements documented with examples and exit codes
- User-level hook configuration (`--user` flag) documented
- Code graph design expanded with action capture and session tracking
- Design documentation restructured with standard headings
- Graph feature design docs reorganized under code-graph-design folder
- README updated with user, project, and local config scope flags

## [0.2.0] - 2026-04-02

### Added
- `init --user` flag to create user-level configuration independent of project config
- Scope definitions now include descriptions, giving contributors context when choosing scopes
- API-level error detection for malformed or incomplete LLM responses

### Changed
- Scope generation produces structured output with name and description fields
- Existing project config is preserved when adding new scopes
- LLM requests automatically retry on token limit exhaustion
- Files are re-staged automatically when a commit fails mid-flow

### Fixed
- User config values now correctly merge into project config
- Token exhaustion handled gracefully with automatic retry
- Empty LLM responses return a clear error instead of failing silently
- Empty commit plans produce a descriptive error message

## [0.1.0] - 2026-03-24

### Added
- Multi-commit workflow that splits staged changes into up to 5 atomic commit groups
- AI-generated conventional commit messages from staged diffs
- `--amend` flag to regenerate the last commit message without re-staging
- `--no-stage` flag to skip auto-staging and commit only already-staged files
- `--free` flag to use built-in credentials with no provider setup
- `config` command group with `config prompts` to inspect active system prompts
- Shell completion for bash, zsh, fish, and PowerShell via `git-agent completion`
- AI-generated `.gitignore` with Node.js template support
- `--co-author` flag for co-author trailers (supports multiple authors)
- `--trailer` flag for arbitrary git trailers
- Conventional commit hook with 3 retries and 2 automatic re-plans on failure
- Commit-msg hook proxy for external hook integration
- Auto-scope generation from git history when no scopes are configured
- Diff filtering and truncation to stay within LLM context limits

### Changed
- Layered config precedence: CLI flag > user config > default
- Composite hook executor supports Go validation, shell scripts, or both
- Hook retries include previous LLM context for better message regeneration
- Concurrent diff collection for faster multi-file performance
- Commit message body lines enforce 72-character wrap

### Fixed
- Diff filter and scope validation errors handled gracefully
- Hook retry preserves pre-trailer message to keep trailers out of LLM context
- Config overrides apply correctly in free mode
- Re-running `init` preserves the existing hook configuration
- Raw diff input sanitized before LLM submission

### Security
- System prompt validation prevents prompt injection
- Model identity masking in proxy responses

[Unreleased]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.12.0...HEAD
[0.12.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.11.1...v0.12.0
[0.11.1]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.11.0...v0.11.1
[0.11.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.10.1...v0.11.0
[0.10.1]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.5.2...v0.6.0
[0.5.2]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/GitAgentHQ/git-agent-cli/releases/tag/v0.1.0
