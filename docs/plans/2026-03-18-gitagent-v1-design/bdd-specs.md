# BDD Specifications — ga V1

## Feature: Project Initialization

```gherkin
Feature: ga init - AI-powered scope detection and project config generation
  As a developer or Coding Agent
  I want to generate .ga/config.yml from my repository's history
  So that ga commit uses accurate, project-specific scopes without manual configuration

  Background:
    Given the `ga` binary is installed
    And I am in a git repository
    And a valid OpenAI-compatible API endpoint is available
```

### Happy Path

```gherkin
  Scenario: Init with default empty hook
    Given the repository has 50+ commits with conventional commit subjects
    And GA_API_KEY is set
    And .ga/config.yml does not exist
    When I run `ga init`
    Then git log subjects (up to 200) are read
    And top-level directories are scanned
    And the LLM returns {"scopes": ["api", "core", "auth"], "reasoning": "..."}
    And .ga/config.yml is written with the scopes list
    And .ga/hooks/pre-commit is created as an empty executable placeholder (exit 0)
    And stdout contains the generated .ga/config.yml content
    And stderr contains the LLM reasoning
    And exit code is 0

  Scenario: Init with built-in conventional hook
    Given .ga/config.yml does not exist
    And GA_API_KEY is set
    When I run `ga init --hook conventional`
    Then .ga/config.yml is written
    And .ga/hooks/pre-commit is installed from the embedded conventional template
    And .ga/hooks/pre-commit is executable (chmod +x)
    And stderr prints "installed hook: conventional"
    And exit code is 0

  Scenario: Unknown hook name
    When I run `ga init --hook unknown-hook`
    Then stderr prints "error: unknown built-in hook \"unknown-hook\" (available: conventional)"
    And no files are written
    And exit code is 1

  Scenario: Init on fresh repository with no commit history
    Given the repository has 0 commits
    And GA_API_KEY is set
    When I run `ga init`
    Then only top-level directories are used as hints
    And the LLM generates scopes from directory names
    And .ga/config.yml is written
    And exit code is 0

  Scenario: Custom max-commits depth
    Given the repository has 500 commits
    When I run `ga init --max-commits 50`
    Then only the 50 most recent commit subjects are sent to the LLM
    And exit code is 0
```

### Error Scenarios

```gherkin
  Scenario: Config already exists without --force
    Given .ga/config.yml already exists
    When I run `ga init`
    Then stderr prints "error: .ga/config.yml already exists (use --force to overwrite)"
    And the existing file is not modified
    And exit code is 1

  Scenario: Hook already exists without --force
    Given .ga/hooks/pre-commit already exists
    When I run `ga init`
    Then stderr prints "error: .ga/hooks/pre-commit already exists (use --force to overwrite)"
    And exit code is 1

  Scenario: Config and hook overwritten with --force
    Given .ga/config.yml and .ga/hooks/pre-commit already exist
    When I run `ga init --force`
    Then both files are overwritten
    And exit code is 0

  Scenario: Not in a git repository
    Given the current directory is not a git repository
    When I run `ga init`
    Then stderr prints "error: not a git repository"
    And exit code is 1

  Scenario: Missing API key
    Given no GA_API_KEY is set and no --api-key flag
    When I run `ga init`
    Then stderr prints "error: API key required (set GA_API_KEY or use --api-key)"
    And exit code is 1
```

---

## Feature: AI-Powered Git Commit

```gherkin
Feature: ga commit - AI-powered semantic commit message generation
  As a developer or Coding Agent
  I want to generate semantic commit messages from staged diffs
  So that my commit history is consistent, descriptive, and non-interactive

  Background:
    Given the `ga` binary is installed
    And I am in a git repository
    And a valid OpenAI-compatible API endpoint is available
```

---

## Happy Path

```gherkin
  Scenario: Generate and commit from staged changes
    Given I have staged changes in the repository
    And GA_API_KEY is set to a valid key
    When I run `ga commit`
    Then the staged diff is extracted via `git diff --staged`
    And the diff is sent to the LLM with a conventional commit prompt
    And the LLM returns {"commit_message": "feat(core): ...", "body": "- ...\n\n...", "outline": "..."}
    And ga assembles the full commit message (title + blank line + body)
    And .ga/hooks/pre-commit (if present) receives the JSON payload and exits 0
    And `git commit -m "<full_commit_message>"` is executed
    And the outline is printed to stdout
    And exit code is 0

  Scenario: Commit with scopes from .ga/config.yml
    Given .ga/config.yml exists with scopes [api, core, auth]
    And I have staged changes in src/api/handler.go
    When I run `ga commit`
    Then the LLM prompt includes "Valid scopes: api, core, auth"
    And the generated commit_message uses one of the valid scopes
    And the hook receives config.scopes = ["api", "core", "auth"]
    And exit code is 0

  Scenario: Generate commit with Co-Authored-By footer
    Given I have staged changes
    And GA_CO_AUTHOR is set to "Claude Sonnet 4.6 <noreply@anthropic.com>"
    When I run `ga commit`
    Then the assembled commit message ends with "Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
    And exit code is 0

  Scenario: Generate commit with user intent
    Given I have staged changes
    And I run `ga commit --intent "fix: resolve token expiry race condition"`
    Then the LLM prompt includes the intent context
    And the generated commit message reflects the intent
    And the commit is created
    And exit code is 0

  Scenario: Dry-run generates message without committing
    Given I have staged changes
    When I run `ga commit --dry-run`
    Then the LLM is called and a message is generated
    And the pre-commit hook (if present) is NOT executed
    And `git commit` is NOT executed
    And stdout contains the generated outline and message
    And the staging area is unchanged
    And exit code is 0
```

---

## Diff Filtering and Truncation

```gherkin
  Scenario: Lock files are excluded from LLM payload
    Given I have staged changes including package-lock.json and src/main.go
    When I run `ga commit`
    Then the diff sent to LLM excludes package-lock.json content
    And the diff includes src/main.go content
    And the commit still includes all staged files (lock file is committed)
    And exit code is 0

  Scenario: Binary files are excluded from LLM payload
    Given I have staged changes including assets/logo.png and src/app.go
    When I run `ga commit`
    Then the diff sent to LLM excludes binary file content
    And the commit includes both files
    And exit code is 0

  Scenario: Diff exceeds max-diff-lines is truncated
    Given I have staged changes totaling 800 lines
    And GA_MAX_DIFF_LINES is not set (default: 500)
    When I run `ga commit`
    Then only 500 lines of diff are sent to the LLM
    And stderr prints "warning: diff truncated to 500 lines (was 800)"
    And a commit is created successfully
    And exit code is 0

  Scenario: Custom max-diff-lines via flag
    Given I have staged changes totaling 300 lines
    When I run `ga commit --max-diff-lines 100`
    Then only 100 lines of diff are sent to the LLM
    And exit code is 0

  Scenario: All staged files are lock/binary (nothing to send LLM)
    Given I have only staged go.sum and *.png files
    When I run `ga commit`
    Then stderr prints "error: no staged text changes after filtering"
    And no LLM call is made
    And exit code is 1
```

---

## Configuration Resolution

```gherkin
  Scenario: API key from flag takes precedence over env var
    Given GA_API_KEY is set to "env-key"
    When I run `ga commit --api-key "flag-key"`
    Then the LLM request uses "flag-key" as the API key
    And exit code is 0

  Scenario: API key from env var when no flag provided
    Given GA_API_KEY is set to "env-key"
    And no --api-key flag is provided
    When I run `ga commit`
    Then the LLM request uses "env-key" as the API key
    And exit code is 0

  Scenario: Custom model via flag
    Given I run `ga commit --model "gpt-4-turbo"`
    Then the LLM API request specifies model "gpt-4-turbo"
    And exit code is 0

  Scenario: Custom OpenAI-compatible base URL
    Given I have a local LLM endpoint at http://localhost:11434/v1
    When I run `ga commit --base-url "http://localhost:11434/v1"`
    Then the LLM API request is sent to http://localhost:11434/v1
    And exit code is 0
```

---

## Environment Error Scenarios

```gherkin
  Scenario: git binary not found
    Given `git` is not installed or not in PATH
    When I run `ga commit`
    Then stderr prints "error: git not found in PATH"
    And no LLM call is made
    And exit code is 1

  Scenario: Not in a git repository
    Given the current directory is not a git repository
    When I run `ga commit`
    Then stderr prints "error: not a git repository"
    And no LLM call is made
    And exit code is 1
```

## Error Scenarios

```gherkin
  Scenario: No staged changes
    Given I have no staged changes in the repository
    When I run `ga commit`
    Then stderr prints "error: no staged changes to commit"
    And no LLM call is made
    And `git commit` is NOT executed
    And exit code is 1

  Scenario: Missing API key
    Given no GA_API_KEY env var is set
    And no --api-key flag is provided
    When I run `ga commit`
    Then stderr prints "error: API key required (set GA_API_KEY or use --api-key)"
    And exit code is 1

  Scenario: LLM API returns HTTP error
    Given I have staged changes
    And the OpenAI API returns HTTP 500
    When I run `ga commit`
    Then stderr prints "error: failed to generate commit message: <details>"
    And `git commit` is NOT executed
    And exit code is 1

  Scenario: LLM API timeout
    Given I have staged changes
    And the OpenAI API does not respond within 30 seconds
    When I run `ga commit`
    Then stderr prints "error: API request timed out"
    And exit code is 1

  Scenario: LLM returns malformed JSON
    Given I have staged changes
    And the LLM returns a response that is not valid JSON
    When I run `ga commit`
    Then stderr prints "error: invalid LLM response format"
    And exit code is 1

  Scenario: LLM returns JSON missing commit_message
    Given I have staged changes
    And the LLM returns {"outline": "..."} without commit_message or body
    When I run `ga commit`
    Then stderr prints "error: LLM response missing required field: commit_message"
    And exit code is 1

  Scenario: LLM returns JSON missing body
    Given I have staged changes
    And the LLM returns {"commit_message": "feat: x", "outline": "..."} without body
    When I run `ga commit`
    Then stderr prints "error: LLM response missing required field: body"
    And exit code is 1
```

---

## Hook System

```gherkin
  Scenario: Pre-commit hook passes validation
    Given I have staged changes
    And .ga/hooks/pre-commit is an executable script that exits 0
    When I run `ga commit`
    Then the hook is executed with JSON payload via stdin
    And the JSON payload matches: {diff, commit_message, intent, staged_files}
    And the commit proceeds
    And exit code is 0

  Scenario: Pre-commit hook blocks commit
    Given I have staged changes
    And .ga/hooks/pre-commit exits with code 1
    And the hook writes "error: WIP commits not allowed" to stderr
    When I run `ga commit`
    Then `git commit` is NOT executed
    And stderr prints the hook's error message
    And exit code is 2

  Scenario: Pre-commit hook does not exist
    Given I have staged changes
    And .ga/hooks/pre-commit does not exist
    When I run `ga commit`
    Then no hook is executed
    And the commit proceeds normally
    And exit code is 0

  Scenario: .ga/hooks directory does not exist
    Given I have staged changes
    And the .ga/hooks directory does not exist
    When I run `ga commit`
    Then no hook execution is attempted
    And the commit proceeds normally
    And exit code is 0

  Scenario: Pre-commit hook is not executable
    Given I have staged changes
    And .ga/hooks/pre-commit exists but has no execute permission (chmod 644)
    When I run `ga commit`
    Then stderr prints "error: hook is not executable: .ga/hooks/pre-commit"
    And `git commit` is NOT executed
    And exit code is 2

  Scenario: Hook receives correct JSON schema
    Given I have staged changes to ["src/main.go", "src/cache.go"]
    And I run `ga commit --intent "add caching"`
    And .ga/hooks/pre-commit captures and validates stdin
    Then the hook stdin JSON contains:
      - diff: (non-empty filtered diff)
      - commit_message: (full assembled message: title + blank line + body + optional Co-Authored-By)
      - intent: "add caching"
      - staged_files: ["src/main.go", "src/cache.go"]
```

---

## Verbose Mode

```gherkin
  Scenario: Verbose flag outputs debug info to stderr
    Given I have staged changes and GA_API_KEY is set
    When I run `ga commit --verbose`
    Then stderr prints "resolved model: gpt-4o"
    And stderr prints "resolved api-key: sk-1234...abcd" (masked)
    And stderr prints "staged files: [src/main.go]"
    And stderr prints "diff lines: 42 (within limit)"
    And stderr prints "calling LLM..."
    And stderr prints "LLM response received"
    And the commit proceeds normally
    And stdout contains only the outline
    And exit code is 0

  Scenario: Verbose flag shows truncation info
    Given I have staged changes totaling 800 lines
    When I run `ga commit --verbose`
    Then stderr includes "diff truncated: 800 → 500 lines"
    And exit code is 0
```

## Exit Code Contract

```gherkin
  Scenario: Success exits with code 0
    Given a successful `ga commit` run
    Then the process exits with code 0
    And stdout contains the outline

  Scenario: General error exits with code 1
    Given any system-level failure (no staged changes, API error, config error)
    Then the process exits with code 1
    And stderr contains a descriptive error message
    And stdout is empty

  Scenario: Hook block exits with code 2
    Given a pre-commit hook rejects the commit
    Then the process exits with code 2
    And stderr contains the hook's rejection message
    And stdout is empty
```

---

## Output Contract (stdout/stderr isolation)

```gherkin
  Scenario: stdout contains only the outline on success
    Given a successful `ga commit` run
    Then stdout contains exactly the outline text and nothing else
    And stdout does not contain the commit message
    And stdout does not contain debug info

  Scenario: All errors and warnings go to stderr
    Given any warning (e.g., diff truncation) occurs
    Then the warning is written to stderr
    And stdout is not affected

  Scenario: Upstream agent can parse stdout directly
    Given ga is invoked by a Coding Agent subprocess
    And the commit succeeds
    When the agent reads stdout
    Then it receives the clean outline string without extra formatting
```

---

## Testing Strategy

### Unit Tests (mock infrastructure)
- `CommitService.Execute()` with mock LLM returning valid/invalid responses
- `CommitService.Execute()` with mock hook returning pass/fail
- `CommitService.Execute()` with empty diff
- Config resolver: flag > env > default precedence
- Diff filter: lock file patterns, binary detection
- Diff truncator: line count boundary, truncation note appended

### Integration Tests (real subprocesses)
- Real git repo in `t.TempDir()`: stage changes → `ga commit` → verify `git log`
- Hook script execution: write real `pre-commit` script to temp dir
- OpenAI client: mock HTTP server (`httptest.NewServer`) returning valid/invalid JSON

### End-to-End Tests
- Full `ga commit` pipeline with mock OpenAI endpoint
- `ga commit --dry-run` with mock endpoint
- Hook blocking with real executable script
