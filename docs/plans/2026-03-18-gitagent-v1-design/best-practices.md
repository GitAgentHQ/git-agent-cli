# Best Practices — ga-cli V1

## Go Idioms

### Error Handling

Use typed errors with exit codes for clean main-loop handling:

```go
// pkg/errors/errors.go
type ExitCode int
const (ExitOK ExitCode = 0; ExitError ExitCode = 1; ExitHook ExitCode = 2)

type DomainError struct {
    Code    ExitCode
    Message string
    Cause   error
}
func (e *DomainError) Unwrap() error { return e.Cause }

// In cmd/commit.go: map domain errors to exit codes
func exitCode(err error) int {
    var de *errors.DomainError
    if errors.As(err, &de) {
        return int(de.Code)
    }
    return 1
}
```

Always wrap subprocess errors with context:

```go
out, err := exec.CommandContext(ctx, "git", "diff", "--staged").Output()
if err != nil {
    return nil, &errors.DomainError{
        Code:    errors.ExitError,
        Message: "failed to read staged changes",
        Cause:   err,
    }
}
```

### Context and Timeouts

Apply a timeout to the LLM call (not the hook — hooks manage their own timeouts):

```go
const llmTimeout = 60 * time.Second

func (s *CommitService) Execute(ctx context.Context, input *CommitInput) (*CommitResult, error) {
    llmCtx, cancel := context.WithTimeout(ctx, llmTimeout)
    defer cancel()
    msg, err := s.generator.Generate(llmCtx, req)
    // ...
}
```

### Interface Injection (Testability)

All external dependencies are injected via constructor:

```go
func NewCommitService(
    git git.Client,
    filter diff.DiffFilter,
    truncator diff.DiffTruncator,
    generator commit.CommitMessageGenerator,
    hooks hook.HookExecutor,
) *CommitService {
    return &CommitService{...}
}
```

Wire dependencies in `main.go` only:

```go
// main.go
gitClient := &gitinfra.Client{}
openaiClient := openaiinfra.NewClient(cfg.APIKey, cfg.BaseURL, cfg.Model)
hookExecutor := &hookinfra.Executor{}
svc := application.NewCommitService(gitClient, filter, truncator, openaiClient, hookExecutor)
```

### Immutable Value Objects

`StagedDiff` and `CommitMessage` are never mutated after creation. Return new instances from filter/truncate:

```go
// DiffFilter returns a new StagedDiff, never modifies the input
func (f *Filter) Filter(ctx context.Context, d *diff.StagedDiff) (*diff.StagedDiff, error) {
    filtered := removeSkippedFiles(d.Raw)
    return &diff.StagedDiff{Raw: filtered, Files: extractFiles(filtered)}, nil
}
```

---

## CLI Design

### stdout/stderr Discipline

| Stream | Content |
|--------|---------|
| stdout | ONLY the commit outline (machine-readable) |
| stderr | Warnings, errors, verbose debug info |

Never mix. This is the agent-compatibility contract.

```go
// Good
fmt.Println(result.Outline)           // stdout
fmt.Fprintln(os.Stderr, "warning: ..") // stderr

// Bad — never write errors to stdout
fmt.Println("Error: " + err.Error())
```

### Flag Naming Convention

| Flag | Env Var | Default |
|------|---------|---------|
| `--api-key` | — | `""` (not required for free endpoint) |
| `--model` | — | `<free default model>` |
| `--base-url` | — | `<free endpoint>` |
| `--co-author` | `GA_CO_AUTHOR` | `""` |
| `--max-diff-lines` | `GA_MAX_DIFF_LINES` | `500` |
| `--intent` / `-i` | — | `""` |
| `--dry-run` | — | `false` |
| `--verbose` | — | `false` |

Flags override `~/.config/ga/config.yml` values. Env vars are only used for `GA_CO_AUTHOR` and `GA_MAX_DIFF_LINES`.

### Cobra Wiring Pattern

```go
func NewCommitCmd(svc *application.CommitService) *cobra.Command {
    var flags CommitFlags

    cmd := &cobra.Command{
        Use:   "commit",
        Short: "Generate and apply an AI commit message",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runCommit(cmd.Context(), svc, flags)
        },
    }

    cmd.Flags().StringVarP(&flags.Intent, "intent", "i", "", "Context hint for LLM")
    cmd.Flags().StringVar(&flags.APIKey, "api-key", "", "API key for custom endpoint")
    // ...
    return cmd
}
```

---

## Hook System

### Security Rules

1. **No shell interpretation**: Always `exec.Command(absPath)` — never `exec.Command("sh", "-c", hookPath)`
2. **Path traversal guard**: Resolve absolute path and verify it's under `.ga/` prefix
3. **Executable check**: Verify `mode&0111 != 0` before execution
4. **stdin only**: Pass context via JSON stdin — not environment variables (avoids leaking API keys)

```go
// CORRECT: direct exec
cmd := exec.CommandContext(ctx, absHookPath)
cmd.Stdin = bytes.NewReader(payload)

// WRONG: shell interpretation (allows injection)
cmd := exec.Command("sh", "-c", hookPath)
```

### Hook Timeout

Hooks should have a reasonable timeout to prevent blocking commits:

```go
hookCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
cmd := exec.CommandContext(hookCtx, absHookPath)
```

### Example Hook (documentation)

```bash
#!/bin/bash
# .ga/hooks/pre-commit
# Blocks WIP commits; commit_message contains the full assembled message

INPUT=$(cat)
FULL_MSG=$(echo "$INPUT" | jq -r '.commit_message')
TITLE=$(echo "$FULL_MSG" | head -1)

# Block WIP commits
if echo "$TITLE" | grep -qi "WIP\|wip"; then
  echo "error: WIP commits are not allowed" >&2
  exit 1
fi

# Validate conventional commit title (type(scope): description, ≤50 chars)
if ! echo "$TITLE" | grep -qE '^(feat|fix|docs|style|refactor|perf|test|chore|build|ci)(\([a-z0-9_-]+\))?!?: [a-z].+'; then
  echo "error: title must be: type(scope): description (all lowercase, ≤50 chars)" >&2
  exit 1
fi

# Validate body exists (lines 3+)
BODY=$(echo "$FULL_MSG" | tail -n +3)
if [ -z "$(echo "$BODY" | tr -d '[:space:]')" ]; then
  echo "error: commit body required (bullet points + explanation)" >&2
  exit 1
fi

exit 0
```

---

## LLM Integration

### Prompt Design Principles

1. **System prompt**: Define the JSON schema explicitly, state "respond ONLY with JSON"
2. **Temperature**: Set to 0 for deterministic output
3. **Max tokens**: Cap at 500 (commit messages are short)
4. **JSON mode**: Always use `ResponseFormat: JSONObject` with go-openai

```go
resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
    Model:       c.model,
    Temperature: 0,
    MaxTokens:   800, // body + explanation needs more headroom than title-only
    ResponseFormat: &openai.ChatCompletionResponseFormat{
        Type: openai.ChatCompletionResponseFormatTypeJSONObject,
    },
    Messages: []openai.ChatCompletionMessage{
        {Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
        {Role: openai.ChatMessageRoleUser, Content: userPrompt},
    },
})
```

### Response Validation

Always validate both required fields before proceeding:

```go
func parseResponse(raw string) (*commit.CommitMessage, error) {
    var r struct {
        CommitMessage string `json:"commit_message"`
        Body          string `json:"body"`
        Outline       string `json:"outline"`
    }
    if err := json.Unmarshal([]byte(raw), &r); err != nil {
        return nil, errors.New(errors.ExitError, "invalid LLM response format")
    }
    if r.CommitMessage == "" {
        return nil, errors.New(errors.ExitError, "LLM response missing required field: commit_message")
    }
    if r.Body == "" {
        return nil, errors.New(errors.ExitError, "LLM response missing required field: body")
    }
    if r.Outline == "" {
        return nil, errors.New(errors.ExitError, "LLM response missing required field: outline")
    }
    return &commit.CommitMessage{Title: r.CommitMessage, Body: r.Body, Outline: r.Outline}, nil
}
```

---

## Security

### API Key Handling

1. Never log the full API key
2. In verbose mode, show only first 4 + last 4 chars
3. Never include in diff output or hook JSON payload

```go
func maskKey(key string) string {
    if len(key) < 8 { return "***" }
    return key[:4] + "..." + key[len(key)-4:]
}
```

### Diff Content Warning

Document in README: staged diffs may contain secrets (API keys, passwords, tokens). Recommend using `git-secrets`, `detect-secrets`, or a custom `.ga/hooks/pre-commit` that scans for patterns.

`ga` itself does not scan for secrets in V1 (avoid false positives from legitimate code).

### git commit Safety

Use `git commit -m` with the message as a direct argument — never interpolate into a shell string:

```go
// CORRECT: message passed as discrete argument
exec.CommandContext(ctx, "git", "commit", "-m", message)

// WRONG: shell injection risk
exec.Command("sh", "-c", "git commit -m \""+message+"\"")
```

---

## Testing

### Mock Pattern

```go
// Domain interface mock for unit tests
type MockCommitGenerator struct {
    GenerateFunc func(ctx context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error)
}

func (m *MockCommitGenerator) Generate(ctx context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error) {
    return m.GenerateFunc(ctx, req)
}
```

### Table-Driven Tests

```go
func TestCommitServiceExitCodes(t *testing.T) {
    tests := []struct {
        name         string
        mockDiff     string
        mockLLMErr   error
        mockHookExit int
        wantExitCode int
    }{
        {"no staged changes", "", nil, 0, 1},
        {"api failure", "diff...", fmt.Errorf("500"), 0, 1},
        {"hook blocked", "diff...", nil, 1, 2},
        {"success", "diff...", nil, 0, 0},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // wire mocks, run service, assert exit code
        })
    }
}
```

### Integration Test: Real Git Repo

```go
func TestCommitIntegration(t *testing.T) {
    if testing.Short() { t.Skip() }

    dir := t.TempDir()
    exec.Command("git", "-C", dir, "init").Run()
    exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
    exec.Command("git", "-C", dir, "config", "user.name", "test").Run()

    // Write a file and stage it
    os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
    exec.Command("git", "-C", dir, "add", "main.go").Run()

    // Run ga commit with mock OpenAI endpoint
    // ... verify git log
}
```

### No Mocking the Hook Runner for Security Tests

Test actual hook execution with real temp scripts:

```go
func TestHookBlocks(t *testing.T) {
    dir := t.TempDir()
    hookDir := filepath.Join(dir, ".aig", "hooks")
    os.MkdirAll(hookDir, 0755)

    script := filepath.Join(hookDir, "pre-commit")
    os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0755)

    executor := &hookinfra.Executor{WorkDir: dir}
    result, err := executor.Execute(context.Background(), "pre-commit", hook.HookInput{...})

    require.NoError(t, err)
    assert.False(t, result.Passed)
    assert.Equal(t, 1, result.ExitCode)
}
```

---

## Performance

### Startup Budget

| Component | Budget |
|-----------|--------|
| Binary startup | < 5ms |
| Flag parsing | < 1ms |
| `git diff --staged` | ~10ms |
| Diff filter + truncate | < 5ms |
| **Total before LLM call** | **< 20ms** |
| LLM API call | 500ms–3s (network) |

### Diff Processing

Line-based truncation is O(n) in diff size — acceptable. Do not use regex on the full diff for filtering; parse diff headers (lines starting with `diff --git`) to identify files, then skip entire sections:

```go
// Efficient: scan line by line, skip sections for matching files
func filterDiff(raw string, skipFn func(filename string) bool) string {
    var out strings.Builder
    var skip bool
    for _, line := range strings.Split(raw, "\n") {
        if strings.HasPrefix(line, "diff --git") {
            filename := extractFilename(line)
            skip = skipFn(filename)
        }
        if !skip {
            out.WriteString(line + "\n")
        }
    }
    return out.String()
}
```

### Binary Size

Keep binary under 15MB. With only cobra + go-openai:
- `go build -ldflags="-s -w"` strips debug info (~30% reduction)
- `upx` compression optional (adds startup decompression cost, skip for <20ms budget)
