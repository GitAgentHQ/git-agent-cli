# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/GitAgentHQ/git-agent-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/GitAgentHQ/git-agent-cli/releases/tag/v0.1.0
