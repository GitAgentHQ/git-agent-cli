# Task 003: Config resolver test

**depends-on**: task-002

## Description

Write tests for the configuration resolution system that handles CLI flags, environment variables, .ga/config.yml, and git config precedence.

## Execution Context

**Task Number**: 3 of 16
**Phase**: Core Features
**Prerequisites**: Domain interfaces defined

## BDD Scenario

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

**Spec Source**: `../2026-03-18-gitagent-v1-design/bdd-specs.md` (Configuration Resolution section)

## Files to Modify/Create

- Create: `infrastructure/config/resolver_test.go`
- Create: `infrastructure/config/resolver.go`

## Steps

### Step 1: Define test structure
- Use testify for assertions
- Use t.Setenv to manipulate env vars in tests

### Step 2: Write tests for API key resolution
- Test flag > env > gitconfig > default precedence
- Test missing API key returns error

### Step 3: Write tests for model resolution
- Test --model flag
- Test GA_MODEL env var
- Test default model

### Step 4: Write tests for base URL resolution
- Test --base-url flag
- Test GA_BASE_URL env var
- Test default for cloudflare provider

### Step 5: Run tests (should fail)
- Verify tests fail because implementation doesn't exist

## Test Verification

```bash
go test ./infrastructure/config/... -v
# Should fail - resolver not implemented
```

## Verification Commands

```bash
go test ./infrastructure/config/... -v
```

## Success Criteria

- Tests cover all config precedence rules
- Tests use test doubles (no real API calls)
- Tests fail with clear error indicating missing implementation
