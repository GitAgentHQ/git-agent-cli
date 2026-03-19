# Task 007: ga commit core flow test

**depends-on**: task-004

## Description

Write tests for the core `ga commit` flow that generates commit messages from staged diffs.

## Execution Context

**Task Number**: 7 of 16
**Phase**: Core Features
**Prerequisites**: Config resolver implemented

## BDD Scenarios

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

**Spec Source**: `../2026-03-18-gitagent-v1-design/bdd-specs.md` (Happy Path section)

## Files to Modify/Create

- Create: `cmd/commit_test.go`
- Create: `application/commit_service_test.go`

## Steps

### Step 1: Create unit tests for commit service
- Test with mock LLM client
- Test with mock git client
- Test with mock hook executor

### Step 2: Create command tests
- Test flag parsing
- Test stdout/stderr separation

### Step 3: Run tests (should fail)
- Verify tests fail because implementation doesn't exist

## Verification Commands

```bash
go test ./cmd/... -v -run TestCommit
go test ./application/... -v -run TestCommit
```

## Success Criteria

- Tests cover all commit happy path scenarios
- Tests use test doubles (mock LLM, mock git, mock hook)
- Tests fail indicating missing implementation
