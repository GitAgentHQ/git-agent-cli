# Task 003: Provider config resolver test

**depends-on**: task-002

## Description

Write tests for the configuration resolution system that handles CLI flags, `~/.config/ga/config.yml` user config, and `.ga/project.yml` precedence.

## Execution Context

**Task Number**: 3 of 16
**Phase**: Core Features
**Prerequisites**: Domain interfaces defined

## BDD Scenario

```gherkin
Scenario: API key from flag takes precedence over config file
  Given ~/.config/ga/config.yml has api_key "file-key"
  When I run `ga commit --api-key "flag-key"`
  Then the LLM request uses "flag-key" as the API key
  And exit code is 0

Scenario: API key from config file when no flag provided
  Given ~/.config/ga/config.yml has api_key "file-key"
  And no --api-key flag is provided
  When I run `ga commit`
  Then the LLM request uses "file-key" as the API key
  And exit code is 0

Scenario: Zero-config uses built-in free endpoint
  Given ~/.config/ga/ does not exist
  When I run `ga commit`
  Then base_url resolves to built-in free endpoint
  And no API key is required
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
- Use temporary directories to simulate `$XDG_CONFIG_HOME`

### Step 2: Write tests for API key resolution
- Test flag > config file > default precedence
- Test missing API key with custom base_url returns error
- Test zero-config (no config file) uses built-in free endpoint

### Step 3: Write tests for model resolution
- Test --model flag
- Test config file model field
- Test default model

### Step 4: Write tests for base URL resolution
- Test --base-url flag
- Test config file base_url field
- Test default built-in free endpoint

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
