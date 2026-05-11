# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/FradSer/git-agent-cli/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/FradSer/git-agent-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/FradSer/git-agent-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/FradSer/git-agent-cli/releases/tag/v0.1.0
