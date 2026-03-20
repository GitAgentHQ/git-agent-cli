# Task 004: Provider config resolver implementation

**depends-on**: task-003

## Description

Implement the configuration resolution system that handles CLI flags, `~/.config/git-agent/config.yml` user config, and `.git-agent/project.yml` with proper precedence.

## Execution Context

**Task Number**: 4 of 16
**Phase**: Core Features
**Prerequisites**: Config resolver tests written

## Implementation Requirements

### Config Resolution Order (highest to lowest)
1. CLI flag
2. `~/.config/git-agent/config.yml` (or `$XDG_CONFIG_HOME/ga/config.yml`)
3. `.git-agent/project.yml` (project)
4. Built-in default (free endpoint)

### Fields to Resolve
- APIKey: flag --api-key, config.yml api_key, "" (free: not required)
- BaseURL: flag --base-url, config.yml base_url, built-in free endpoint
- Model: flag --model, config.yml model, built-in free model
- Intent: flag --intent
- CoAuthor: flag --co-author
- MaxLines: flag --max-diff-lines, default 500
- DryRun: flag --dry-run
- Verbose: flag --verbose
- Scopes: from .git-agent/project.yml only

### Built-in Defaults
- base_url: project-maintained free endpoint
- model: project default free model
- api_key: "" (not required for free endpoint)

## Files to Modify/Create

- Modify: `infrastructure/config/resolver.go` (implement)
- Create: `infrastructure/config/user.go`
- Create: `infrastructure/config/config_home.go`
- Create: `infrastructure/config/project.go`

## Steps

### Step 1: Implement config home resolver
- Resolve config root via `$XDG_CONFIG_HOME/ga`, fallback to `~/.config/ga`
- Create path helper for `config.yml`

### Step 2: Implement user config reader
- Read `~/.config/git-agent/config.yml` if exists
- Parse YAML for base_url, api_key, model
- Return empty user config if file absent

### Step 3: Implement project config reader
- Read `.git-agent/project.yml` if exists
- Parse YAML for scopes

### Step 4: Implement resolver logic
- Implement priority chain: flag > user config file > built-in default
- Validate: if custom base_url is set, require api_key

### Step 5: Run tests
- Verify all tests pass

## Verification Commands

```bash
go test ./infrastructure/config/... -v
```

## Success Criteria

- All config resolver tests pass
- Correct precedence order implemented
- Clear error messages for missing required fields
