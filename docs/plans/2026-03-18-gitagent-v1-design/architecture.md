# Architecture — ga-cli V1

## Overview

`ga` is a Go CLI application following a DDD-inspired layered architecture. The four layers — `cmd`, `domain`, `application`, `infrastructure` — enforce strict dependency direction: outer layers depend on inner layers, never the reverse.

```
cmd → application → domain ← infrastructure
```

`infrastructure` implements `domain` interfaces. `application` orchestrates domain operations. `cmd` handles I/O formatting and Cobra wiring.

---

## Package Responsibilities

### `cmd/` — CLI Wiring

| File | Responsibility |
|------|---------------|
| `root.go` | Root cobra command; global flag registration |
| `commit.go` | `ga commit` subcommand; parse flags → call `CommitService` → format output |

**Principle**: `cmd` contains zero business logic. It translates CLI flags into `CommitInput` DTO, calls `CommitService.Execute()`, then writes result to stdout/stderr and sets exit code.

```go
func runCommit(cmd *cobra.Command, args []string) {
    input := buildCommitInput(cmd) // parse flags
    result, err := commitSvc.Execute(cmd.Context(), input)
    if err != nil {
        fmt.Fprintln(os.Stderr, err.Error())
        os.Exit(exitCode(err))
    }
    fmt.Println(result.Outline) // stdout only
}
```

---

### `domain/` — Business Contracts

No external dependencies. Defines value objects, interfaces, and domain errors only.

#### `domain/commit/`
```go
// CommitMessage is an immutable value object
type CommitMessage struct {
    Title   string // "feat(core): add cache layer" (≤50 chars, lowercase)
    Body    string // "- Add Redis client\n\nReduces API latency by caching."
    Outline string // "1. Added Redis client; 2. Integrated with service" (stdout only)
}

// CommitMessageGenerator is the LLM abstraction
type CommitMessageGenerator interface {
    Generate(ctx context.Context, req GenerateRequest) (*CommitMessage, error)
}

type GenerateRequest struct {
    Diff        string
    Intent      string
    StagedFiles []string
}
```

#### `domain/diff/`
```go
// StagedDiff is immutable — never mutated after creation
type StagedDiff struct {
    Raw       string
    LineCount int
    Files     []string
}

type DiffFilter interface {
    Filter(ctx context.Context, diff *StagedDiff) (*StagedDiff, error)
}

type DiffTruncator interface {
    // Returns (truncated, wasTruncated, error)
    Truncate(ctx context.Context, diff *StagedDiff, maxLines int) (*StagedDiff, bool, error)
}
```

#### `domain/project/`

```go
// ProjectConfig is the value object written to .ga/config.yml
type ProjectConfig struct {
    Scopes    []string `yaml:"scopes"`
    Reasoning string   `yaml:"-"` // LLM explanation, printed to stderr only
}

type GenerateScopesRequest struct {
    CommitSubjects []string
    Dirs           []string
}

// ScopeGenerator is the LLM abstraction for ga init
type ScopeGenerator interface {
    GenerateScopes(ctx context.Context, req GenerateScopesRequest) (*ProjectConfig, error)
}
```

#### `domain/hook/`
```go
// HookInput is the JSON payload sent to hook scripts via stdin
type HookInput struct {
    Diff          string   `json:"diff"`
    CommitMessage string   `json:"commit_message"`
    Intent        string   `json:"intent"`
    StagedFiles   []string `json:"staged_files"`
}

type HookResult struct {
    Passed       bool
    ExitCode     int
    StderrMsg    string
    HookNotFound bool // not an error — hook is optional
}

type HookExecutor interface {
    Execute(ctx context.Context, hookName string, input HookInput) (*HookResult, error)
}
```

---

### `application/` — Orchestration

Two services: `CommitService` and `InitService`. Neither has knowledge of CLI, OpenAI, or Git — only domain interfaces.

#### `InitService`

```go
type InitService struct {
    git       git.LogReader           // infrastructure interface
    fs        fs.DirScanner           // infrastructure interface
    generator project.ScopeGenerator  // domain interface (LLM)
    writer    fs.ConfigWriter          // infrastructure interface
}

func (s *InitService) Execute(ctx context.Context, input *InitInput) (*InitResult, error) {
    // 1. Read recent commit subjects
    subjects, err := s.git.LogSubjects(ctx, input.MaxCommits)

    // 2. Scan top-level directories as supplementary hints
    dirs, err := s.fs.TopLevelDirs(ctx)

    // 3. Call LLM to suggest scopes
    cfg, err := s.generator.GenerateScopes(ctx, project.GenerateScopesRequest{
        CommitSubjects: subjects,
        Dirs:           dirs,
    })
    // cfg = ProjectConfig{Scopes: [...], Reasoning: "..."}

    // 4. Write .ga/config.yml (respects Force flag)
    err = s.writer.WriteConfig(ctx, cfg, input.Force)

    // 5. Resolve and install hook template
    hookBytes, err := s.hookRegistry.Resolve(input.HookName) // "conventional" or ""
    err = s.writer.InstallHook(ctx, hookBytes, input.Force)  // chmod +x

    return &InitResult{Config: cfg, InstalledHook: input.HookName}, nil
}
```

#### `CommitService`

```go
type CommitService struct {
    git       git.Client
    filter    diff.DiffFilter
    truncator diff.DiffTruncator
    generator commit.CommitMessageGenerator
    hooks     hook.HookExecutor
}

func (s *CommitService) Execute(ctx context.Context, input *CommitInput) (*CommitResult, error) {
    // 1. Read staged diff
    staged, err := s.git.DiffStaged(ctx)
    if staged.Raw == "" {
        return nil, errors.New(errors.ExitError, "no staged changes to commit")
    }

    // 2. Filter
    filtered, err := s.filter.Filter(ctx, staged)

    // 3. Truncate
    truncated, wasTruncated, err := s.truncator.Truncate(ctx, filtered, input.MaxDiffLines)
    if wasTruncated {
        // signal to cmd layer to emit warning to stderr
    }

    // 4. Generate commit message
    msg, err := s.generator.Generate(ctx, commit.GenerateRequest{
        Diff:        truncated.Raw,
        Intent:      input.Intent,
        StagedFiles: truncated.Files,
    })

    // 5. Assemble full commit message
    fullMsg := assembleCommitMessage(msg.Title, msg.Body, input.CoAuthor)

    // 6. Run pre-commit hook (optional)
    if !input.DryRun {
        result, err := s.hooks.Execute(ctx, "pre-commit", hook.HookInput{
            Diff:          truncated.Raw,
            CommitMessage: fullMsg, // full message so hooks can validate body/footer
            Intent:        input.Intent,
            StagedFiles:   truncated.Files,
        })
        if !result.Passed {
            return nil, errors.New(errors.ExitHook, result.StderrMsg)
        }
    }

    // 7. Commit (unless dry-run)
    if !input.DryRun {
        err = s.git.Commit(ctx, fullMsg)
    }

    return &CommitResult{
        Outline:      msg.Outline,
        DryRun:       input.DryRun,
        WasTruncated: wasTruncated,
    }, nil
}

// assembleCommitMessage builds: title + blank line + body [+ blank line + Co-Authored-By]
func assembleCommitMessage(title, body, coAuthor string) string {
    msg := title + "\n\n" + body
    if coAuthor != "" {
        msg += "\n\nCo-Authored-By: " + coAuthor
    }
    return msg
}
```

---

### `infrastructure/` — I/O Adapters

Implements domain interfaces. May import external packages.

#### `infrastructure/git/`

```go
// Thin wrapper around git CLI subprocess
type Client struct{}

func (c *Client) DiffStaged(ctx context.Context) (*diff.StagedDiff, error) {
    out, err := exec.CommandContext(ctx, "git", "diff", "--staged").Output()
    // parse files list from diff headers
    return &diff.StagedDiff{Raw: string(out), ...}, err
}

func (c *Client) Commit(ctx context.Context, message string) error {
    return exec.CommandContext(ctx, "git", "commit", "-m", message).Run()
}
```

#### `infrastructure/openai/`

```go
// Wraps sashabaranov/go-openai with JSON response format
type Client struct {
    openaiClient *openai.Client
    model        string
}

func (c *Client) Generate(ctx context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error) {
    prompt := buildPrompt(req.Diff, req.Intent, req.StagedFiles)
    resp, err := c.openaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model:       c.model,
        Temperature: 0,
        MaxTokens:   800,
        Messages: []openai.ChatCompletionMessage{
            {Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
            {Role: openai.ChatMessageRoleUser, Content: prompt},
        },
        ResponseFormat: &openai.ChatCompletionResponseFormat{
            Type: openai.ChatCompletionResponseFormatTypeJSONObject,
        },
    })
    // parse JSON → CommitMessage
}
```

#### `infrastructure/hooks/` — Built-in Hook Registry

Built-in hook templates are embedded directly in the binary via Go's `embed` package — no runtime files needed.

```go
// registry.go
//go:embed ../hooks/*.sh
var embeddedHooks embed.FS

var builtinHooks = map[string]string{
    "conventional": "hooks/conventional.sh",
    // future: "no-wip", "secret-scan", ...
}

type Registry struct{}

// Resolve returns template bytes for the given hook name.
// Empty name returns the blank placeholder.
func (r *Registry) Resolve(name string) ([]byte, error) {
    if name == "" {
        return embeddedHooks.ReadFile("hooks/empty.sh")
    }
    path, ok := builtinHooks[name]
    if !ok {
        return nil, fmt.Errorf("unknown built-in hook %q (available: %s)",
            name, strings.Join(availableHooks(), ", "))
    }
    return embeddedHooks.ReadFile(path)
}
```

**`hooks/empty.sh`** (default):
```bash
#!/bin/sh
# .ga/hooks/pre-commit — generated by ga init
# Receives JSON via stdin: {diff, commit_message, intent, staged_files, config}
# Exit 0 to allow commit; exit non-zero to block.
exit 0
```

**`hooks/conventional.sh`** (built-in validator):
```bash
#!/bin/sh
# .ga/hooks/pre-commit — conventional commit validator
# Built-in hook installed by: ga init --hook conventional

INPUT=$(cat)
FULL_MSG=$(echo "$INPUT" | jq -r '.commit_message')
TITLE=$(echo "$FULL_MSG" | head -1)
VALID_TYPES="feat|fix|docs|refactor|perf|test|chore|build|ci|style"

# 1. Title format: type(scope): description (lowercase, ≤50 chars)
if ! echo "$TITLE" | grep -qE "^($VALID_TYPES)(\([a-z0-9_-]+\))?!?:[[:space:]][a-z].+"; then
  echo "error: title must match type(scope): description (all lowercase)" >&2
  exit 1
fi

if [ ${#TITLE} -gt 50 ]; then
  echo "error: title must be ≤50 characters (${#TITLE})" >&2
  exit 1
fi

# 2. Body required (lines after blank line)
BODY=$(echo "$FULL_MSG" | tail -n +3)
if [ -z "$(echo "$BODY" | tr -d '[:space:]')" ]; then
  echo "error: body required (bullet points + explanation paragraph)" >&2
  exit 1
fi

# 3. Scope whitelist (if scopes configured)
SCOPES=$(echo "$INPUT" | jq -r '.config.scopes // [] | .[]' 2>/dev/null)
if [ -n "$SCOPES" ]; then
  SCOPE=$(echo "$TITLE" | grep -oE '\([a-z0-9_-]+\)' | tr -d '()')
  if [ -n "$SCOPE" ] && ! echo "$SCOPES" | grep -q "^$SCOPE$"; then
    echo "error: scope '$SCOPE' not in whitelist: $(echo "$SCOPES" | tr '\n' ' ')" >&2
    exit 1
  fi
fi

exit 0
```

#### `infrastructure/hook/` — Hook Executor

```go
// Executes .ga/hooks/<hookName> with security checks
func (e *Executor) Execute(ctx context.Context, hookName string, input hook.HookInput) (*hook.HookResult, error) {
    hookPath := filepath.Join(".aig", "hooks", hookName)

    // 1. Existence check (optional hook)
    if _, err := os.Stat(hookPath); os.IsNotExist(err) {
        return &hook.HookResult{HookNotFound: true, Passed: true}, nil
    }

    // 2. Path traversal guard
    abs, _ := filepath.Abs(hookPath)
    gaAbs, _ := filepath.Abs(".ga")
    if !strings.HasPrefix(abs, gaAbs) {
        return nil, fmt.Errorf("hook path traversal denied")
    }

    // 3. Executable check
    info, _ := os.Stat(hookPath)
    if info.Mode()&0111 == 0 {
        return nil, errors.New(errors.ExitHook, "hook is not executable: "+hookPath)
    }

    // 4. Build JSON payload
    payload, _ := json.Marshal(input)

    // 5. Execute (no shell — direct exec)
    var stderr bytes.Buffer
    cmd := exec.CommandContext(ctx, abs)
    cmd.Stdin = bytes.NewReader(payload)
    cmd.Stderr = &stderr

    err := cmd.Run()
    if err != nil {
        exitCode := 1
        if exitErr, ok := err.(*exec.ExitError); ok {
            exitCode = exitErr.ExitCode()
        }
        return &hook.HookResult{
            Passed:    false,
            ExitCode:  exitCode,
            StderrMsg: stderr.String(),
        }, nil
    }

    return &hook.HookResult{Passed: true, ExitCode: 0}, nil
}
```

#### `infrastructure/config/`

Four-layer resolver: flag → env → `.ga/config.yml` → `git config ga.*` → default.

```go
// resolver.go: flag → env → gitconfig → default (for scalar values)
type Resolver struct {
    flags      *pflag.FlagSet
    projectCfg *ProjectConfig // loaded from .ga/config.yml
}

func (r *Resolver) GetString(flagName, envName, gitKey, defaultVal string) string {
    if val, _ := r.flags.GetString(flagName); val != "" {
        return val
    }
    if val := os.Getenv(envName); val != "" {
        return val
    }
    if val := readGitConfig(gitKey); val != "" { // git config --get ga.<key>
        return val
    }
    return defaultVal
}

// gitconfig.go: reads personal machine defaults via git subprocess
func readGitConfig(key string) string {
    out, err := exec.Command("git", "config", "--get", key).Output()
    if err != nil {
        return "" // missing key is not an error
    }
    return strings.TrimSpace(string(out))
}

// project.go: reads .ga/config.yml (team config, version-controlled)
type ProjectConfig struct {
    Scopes []string `yaml:"scopes"`
}

func LoadProjectConfig() (*ProjectConfig, error) {
    data, err := os.ReadFile(".ga/config.yml")
    if os.IsNotExist(err) {
        return &ProjectConfig{}, nil // optional file
    }
    var cfg ProjectConfig
    return &cfg, yaml.Unmarshal(data, &cfg)
}
```

`model` and `co-author` are **not** read from gitconfig — they are per-commit and must be supplied via flag or env.

---

### `pkg/` — Shared Utilities

#### `pkg/errors/`

```go
type ExitCode int

const (
    ExitOK   ExitCode = 0
    ExitError ExitCode = 1
    ExitHook ExitCode = 2
)

type DomainError struct {
    Code    ExitCode
    Message string
    Cause   error
}

func (e *DomainError) Error() string { ... }
func (e *DomainError) Unwrap() error { return e.Cause }

func New(code ExitCode, msg string) *DomainError {
    return &DomainError{Code: code, Message: msg}
}
```

#### `pkg/filter/`

Lock file and binary patterns:

```go
var SkipPatterns = []string{
    "package-lock.json", "yarn.lock", "Cargo.lock",
    "composer.lock", "Gemfile.lock", "go.sum",
    "*.png", "*.jpg", "*.gif", "*.bin", "*.exe", "*.so",
}
```

---

## Dependency Graph

```
main.go
  └── cmd/root.go + cmd/commit.go
        └── application/commit_service.go
              ├── domain/commit/generator.go     (interface)
              ├── domain/diff/filter.go          (interface)
              ├── domain/diff/truncator.go       (interface)
              ├── domain/hook/executor.go        (interface)
              └── infrastructure/* (injected at main.go)
                    ├── infrastructure/git/
                    ├── infrastructure/openai/
                    ├── infrastructure/hook/
                    └── infrastructure/config/
```

---

## go.mod

```
module github.com/fradser/ga-cli

go 1.23

require (
    github.com/spf13/cobra v1.8.0
    github.com/sashabaranov/go-openai v1.24.0
    gopkg.in/yaml.v3 v3.0.1
)
```

`gopkg.in/yaml.v3` is the only addition — used solely for `.ga/config.yml` parsing.

---

## LLM Prompt Design

### System Prompt
```
You are an expert software engineer. Generate a conventional commit message
from the provided git diff. Respond ONLY with valid JSON matching this schema:
{"commit_message": "string", "body": "string", "outline": "string"}

Rules:
- commit_message: conventional commits format (type(scope): subject)
  - ALL LOWERCASE, ≤50 chars, imperative mood, no trailing period
  - Add "!" before ":" for breaking changes
  - Valid types: feat, fix, docs, refactor, perf, test, chore, build, ci, style
- body: two sections separated by a blank line:
  1. Bullet points: "- Verb Object Detail" (imperative, ≤72 chars/line, 1-5 items)
  2. Explanation paragraph: 1-3 sentences explaining WHY, not just what
- outline: numbered summary for machine consumption (1-3 items)
- If intent is provided, use it to guide the commit type/subject
```

### User Prompt Template
```
Git diff:
<diff>
{{.Diff}}
</diff>

{{if .Intent}}User intent: {{.Intent}}{{end}}

Staged files: {{join .StagedFiles ", "}}
```

---

## Security Considerations

1. **Hook path traversal**: `filepath.Abs` + prefix check against `.ga/` absolute path
2. **Hook execution**: Direct `exec.Command` — never `sh -c hookPath` (prevents injection)
3. **API key masking**: Never log full API key; use `key[:4]+"..."` in verbose output
4. **Diff content**: Document that diffs may contain secrets; recommend `git-secrets` or `detect-secrets` as pre-commit hooks
5. **Hook permissions**: Validate `mode&0111 != 0` before execution; warn if not owned by current user

---

## Testing Strategy

| Layer | Approach |
|-------|----------|
| `domain/` | Pure unit tests — no mocks needed (pure logic) |
| `application/` | Unit tests with mock interfaces (MockLLM, MockGit, MockHook) |
| `infrastructure/git/` | Integration tests with real temp git repo (`t.TempDir()`) |
| `infrastructure/openai/` | Unit tests with mock HTTP server (`httptest.NewServer`) |
| `infrastructure/hook/` | Unit tests with real executable temp scripts |
| `cmd/` | End-to-end tests with injected mocks |
