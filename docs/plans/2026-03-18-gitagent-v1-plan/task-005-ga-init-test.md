# Task 005: ga init command test

**depends-on**: task-004

## Description

Write tests for the `ga init` command that generates .ga/config.yml from git history and top-level directories using an LLM.

## Execution Context

**Task Number**: 5 of 16
**Phase**: Core Features
**Prerequisites**: Config resolver implemented

## BDD Scenarios

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

**Spec Source**: `../2026-03-18-gitagent-v1-design/bdd-specs.md` (Feature: Project Initialization)

## Files to Modify/Create

- Create: `cmd/init_test.go`
- Create: `application/init_service_test.go`

## Steps

### Step 1: Create unit tests for init service
- Test with mock LLM client
- Test with mock git client
- Test with mock fs

### Step 2: Create command tests
- Test flag parsing
- Test error cases

### Step 3: Run tests (should fail)
- Verify tests fail because implementation doesn't exist

## Verification Commands

```bash
go test ./cmd/... -v -run TestInit
go test ./application/... -v -run TestInit
```

## Success Criteria

- Tests cover all init scenarios
- Tests use test doubles (mock LLM, mock git)
- Tests fail indicating missing implementation
