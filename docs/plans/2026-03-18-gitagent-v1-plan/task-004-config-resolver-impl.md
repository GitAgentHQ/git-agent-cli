# Task 004: Config resolver implementation

**depends-on**: task-003

## Description

Implement the configuration resolution system that handles CLI flags, environment variables, .ga/config.yml, and git config with proper precedence.

## Execution Context

**Task Number**: 4 of 16
**Phase**: Core Features
**Prerequisites**: Config resolver tests written

## Implementation Requirements

### Config Resolution Order (highest to lowest)
1. CLI flag
2. GA_* environment variable
3. .ga/config.yml (project)
4. git config ga.* (global)
5. Default value

### Fields to Resolve
- APIKey (required): flag --api-key, env GA_API_KEY, git config ga.apikey
- BaseURL: flag --base-url, env GA_BASE_URL, git config ga.baseurl, default (cloudflare)
- Model: flag --model, env GA_MODEL, default @cf/openai/gpt-oss-20b
- Provider: flag --provider, env GA_PROVIDER, default cloudflare
- AccountID: flag --account-id, env GA_ACCOUNT_ID, required for cloudflare
- Intent: flag --intent, env GA_INTENT
- CoAuthor: flag --co-author, env GA_CO_AUTHOR
- MaxLines: flag --max-diff-lines, env GA_MAX_DIFF_LINES, default 500
- DryRun: flag --dry-run
- Verbose: flag --verbose, env GA_VERBOSE
- Scopes: from .ga/config.yml only

### Provider Defaults
- cloudflare: https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/v1
- openai: https://api.openai.com/v1

## Files to Modify/Create

- Modify: `infrastructure/config/resolver.go` (implement)
- Create: `infrastructure/config/gitconfig.go`
- Create: `infrastructure/config/project.go`

## Steps

### Step 1: Implement gitconfig reader
- Use `git config --get` subprocess to read ga.* values
- Return empty string if not set

### Step 2: Implement project config reader
- Read .ga/config.yml if exists
- Parse YAML for scopes

### Step 3: Implement resolver logic
- Implement priority chain: flag > env > project > gitconfig > default

### Step 4: Run tests
- Verify all tests pass

## Verification Commands

```bash
go test ./infrastructure/config/... -v
```

## Success Criteria

- All config resolver tests pass
- Correct precedence order implemented
- Clear error messages for missing required fields
