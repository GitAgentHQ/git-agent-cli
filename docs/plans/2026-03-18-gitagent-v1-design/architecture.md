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
    Message string // "feat(core): add cache layer"
    Outline string // "1. Added Redis client; 2. Integrated with service"
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

Single service: `CommitService`. Owns the full workflow. Has no knowledge of CLI, OpenAI, or Git — only domain interfaces.

```go
type CommitService struct {
    git        git.Client          // infrastructure interface
    filter     diff.DiffFilter     // domain interface
    truncator  diff.DiffTruncator  // domain interface
    generator  commit.CommitMessageGenerator
    hooks      hook.HookExecutor
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

    // 5. Run pre-commit hook (optional)
    if !input.DryRun {
        result, err := s.hooks.Execute(ctx, "pre-commit", hook.HookInput{
            Diff:          truncated.Raw,
            CommitMessage: msg.Message,
            Intent:        input.Intent,
            StagedFiles:   truncated.Files,
        })
        if !result.Passed {
            return nil, errors.New(errors.ExitHook, result.StderrMsg)
        }
    }

    // 6. Commit (unless dry-run)
    if !input.DryRun {
        err = s.git.Commit(ctx, msg.Message)
    }

    return &CommitResult{
        Outline:     msg.Outline,
        DryRun:      input.DryRun,
        WasTruncated: wasTruncated,
    }, nil
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
        Model: c.model,
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

#### `infrastructure/hook/`

```go
// Executes .aig/hooks/<hookName> with security checks
func (e *Executor) Execute(ctx context.Context, hookName string, input hook.HookInput) (*hook.HookResult, error) {
    hookPath := filepath.Join(".aig", "hooks", hookName)

    // 1. Existence check (optional hook)
    if _, err := os.Stat(hookPath); os.IsNotExist(err) {
        return &hook.HookResult{HookNotFound: true, Passed: true}, nil
    }

    // 2. Path traversal guard
    abs, _ := filepath.Abs(hookPath)
    aigAbs, _ := filepath.Abs(".aig")
    if !strings.HasPrefix(abs, aigAbs) {
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

```go
// Resolver: flag → env → default
type Resolver struct {
    flags *pflag.FlagSet
}

func (r *Resolver) GetString(flagName, envName, defaultVal string) string {
    if val, _ := r.flags.GetString(flagName); val != "" {
        return val
    }
    if val := os.Getenv(envName); val != "" {
        return val
    }
    return defaultVal
}
```

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
)
```

No other direct runtime dependencies.

---

## LLM Prompt Design

### System Prompt
```
You are an expert software engineer. Generate a conventional commit message
from the provided git diff. Respond ONLY with valid JSON matching this schema:
{"commit_message": "string", "outline": "string"}

Rules:
- commit_message: conventional commits format (type(scope): subject)
- subject: imperative mood, max 72 chars, no trailing period
- outline: numbered list of key changes (1-3 items), in the diff's language
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

1. **Hook path traversal**: `filepath.Abs` + prefix check against `.aig/` absolute path
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
