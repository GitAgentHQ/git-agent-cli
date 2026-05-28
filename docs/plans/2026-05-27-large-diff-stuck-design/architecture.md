# Architecture

System overview, component-level changes, interfaces, and dependency
directions for the large-diff "stuck" remediation. Layer ordering:
`cmd → application → domain ← infrastructure`. No inner-to-outer
dependencies are introduced.

## 1. System overview

```
                                     [user shell]
                                          |
                                       SIGINT
                                          |
                                          v
+-----------+    +---------------+    +-----------+    +---------------+
| main.go   |--->| cmd/root.go   |--->| cmd/      |--->| application/  |
| signal.   |    | ExecuteContext|    | commit.go |    | commit_       |
| NotifyCtx |    |  (ctx)        |    |           |    | service.go    |
+-----------+    +---------------+    +-----------+    +-------+-------+
                                                               |
                                  +----------------------------+
                                  |                            |
                                  v                            v
                       +----------+------------+   +-----------+----------+
                       | infrastructure/git/   |   | infrastructure/      |
                       | client.go             |   | openai/client.go     |
                       | + StagedDiffStat(ctx) |   | + http.Client.Timeout|
                       +-----------------------+   | + heartbeat goroutine|
                                                   | + ceiling-aware retry|
                                                   +-----------+----------+
                                                               |
                                                               v
                                                        +------+------+
                                                        | LLM endpoint|
                                                        +-------------+

domain/commit (zero external imports):
  - CommitPlanner            (existing)
  - HeuristicPlanner         (new, parallel interface)
  - ErrPlannerBudgetExhausted, PlannerBudgetExhaustedError (new typed error)
```

## 2. Component changes by layer

### 2.1 `cmd/` and `main.go` (composition root)

**`main.go`** — replace the body with a four-line context wrapper:

```go
package main

import (
    "context"
    "os"
    "os/signal"
    "syscall"

    "github.com/gitagenthq/git-agent/cmd"
)

func main() {
    ctx, stop := signal.NotifyContext(
        context.Background(),
        os.Interrupt, syscall.SIGTERM,
    )
    defer stop()
    cmd.ExecuteContext(ctx)
}
```

**`cmd/root.go`** — keep `Execute()` for the existing exit-code mapping
path; add `ExecuteContext(ctx context.Context)` that calls
`rootCmd.ExecuteContext(ctx)` and reuses the same exit-code mapping. All
existing `cmd.Context()` consumers inside `runCommit` automatically
inherit the signal-aware context with no per-call-site change.

**`cmd/commit.go`** — three additions:

1. Resolve `request_timeout`, `heartbeat_interval` from
   `infraConfig.Resolve(...)` and pass into `infraOpenAI.NewClient`.
2. Resolve `plan_fallback` from `projCfg` and pass into
   `NewCommitService` (new constructor arg).
3. Error-path switch arm for `*commit.PlannerBudgetExhaustedError`:
   render the actionable message to stderr, return exit code 1 via
   `agentErrors.NewExitCodeError(1, ...)`.

### 2.2 `application/commit_service.go`

**`CommitGitClient` interface** — add one method:

```go
type CommitGitClient interface {
    // ... existing methods ...
    StagedDiffStat(ctx context.Context) (string, error)
}
```

**`CommitService` struct** — add two optional collaborators:

```go
type CommitService struct {
    // ... existing fields ...
    heuristicPlanner commit.HeuristicPlanner // nil = no fallback
    out              io.Writer               // legacy field, repurposed
}
```

**`CommitRequest`** — no field changes. `request_timeout` and
`heartbeat_interval` are wired at the LLM client, not on the request,
because they describe transport behaviour not commit behaviour.
`plan_fallback` is read off `project.Config`.

**Per-group block (around `commit_service.go:386-401`)** — insert the
single-file saturation check immediately after the truncator returns:

```go
if didTruncate &&
    len(genDiff.Files) == 1 &&
    len(genDiff.Content) == maxBytes {
    stat, statErr := s.git.StagedDiffStat(ctx)
    if statErr == nil {
        synopsis := buildSynopsis(genDiff.Files[0], stat,
            len(groupDiff.Content), maxBytes)
        genDiff = &diff.StagedDiff{
            Files:   genDiff.Files,
            Content: synopsis,
            Lines:   strings.Count(synopsis, "\n"),
        }
        s.out(req, "commit %d/%d: DIFF-SYNOPSIS fallback (%s)",
            i+1, totalGroups, genDiff.Files[0])
    } else {
        s.vlog(req, "stat fallback failed (continuing with truncated diff): %v", statErr)
    }
}
```

`buildSynopsis` lives in the application package next to the service —
it produces a small string with no domain coupling. The function and the
saturation rule together are the entirety of REQ-007.

**Plan error handling (around lines 281-283 and 312-320)** — wrap the
existing call sites in:

```go
plan, err = s.planner.Plan(ctx, planReq)
if err != nil {
    if errors.Is(err, commit.ErrPlannerBudgetExhausted) &&
        s.heuristicPlanner != nil &&
        req.Config != nil &&
        req.Config.PlanFallback == project.PlanFallbackHeuristic {
        s.out(req, "planner exhausted budget — falling back to directoryBucketer")
        plan, err = s.heuristicPlanner.Plan(ctx, planReq)
    }
    if err != nil {
        return nil, fmt.Errorf("plan commits: %w", err)
    }
}
```

The wrapped `%w` preserves the typed error for the cmd-layer switch arm,
and the fallback is only attempted when explicitly opted in.

**`s.vlog` → `s.out` promotions** — eight sites:

| File:line (existing) | Current | Promoted to `s.out` (always-on) |
|---|---|---|
| `commit_service.go:242` | `auto-generating scopes...` | yes (high-level phase) |
| `commit_service.go:245` | `scope generation failed (continuing without scopes): %v` | yes (degradation must be visible) |
| `commit_service.go:274` | `planning commits...` | yes (primary long-wait phase) |
| `commit_service.go:291` | `plan has %d groups — capping to %d` | yes (observable behaviour shift) |
| `commit_service.go:298` | `unscoped groups detected — refreshing project scopes...` | yes (explains second wait) |
| `commit_service.go:311` | `updated scopes: %v — re-planning...` | yes (explains third wait) |
| `commit_service.go:338` | `planned %d commit(s)` | yes (sets loop expectation) |
| `commit_service.go:416` | `calling LLM... (attempt %d/%d)` | yes, restated as `commit %d/%d: drafting message (attempt %d/%d)` |
| `commit_service.go:561` | `diff truncated (%s)` (amend path) | yes |

All other `vlog` sites stay verbose-only.

### 2.3 `infrastructure/openai/client.go`

**`Client` struct** — add transport-level state:

```go
type Client struct {
    inner          *goopenai.Client
    model          string
    requestTimeout time.Duration
    heartbeatInterval time.Duration
    out            io.Writer
}
```

**`NewClient` signature** — extended:

```go
func NewClient(
    apiKey, baseURL, model string,
    requestTimeout, heartbeatInterval time.Duration,
    out io.Writer,
) *Client {
    cfg := goopenai.DefaultConfig(apiKey)
    if baseURL != "" {
        cfg.BaseURL = baseURL
    }
    if requestTimeout <= 0 {
        requestTimeout = 90 * time.Second
    }
    if heartbeatInterval <= 0 {
        heartbeatInterval = 15 * time.Second
    }
    cfg.HTTPClient = &http.Client{Timeout: requestTimeout}
    return &Client{
        inner:             goopenai.NewClientWithConfig(cfg),
        model:             model,
        requestTimeout:    requestTimeout,
        heartbeatInterval: heartbeatInterval,
        out:               out,
    }
}
```

The `*http.Client` satisfies `goopenai.HTTPDoer` (verified against the
local module cache at v1.41.2: `config.go:32-49`).

**Per-endpoint ceiling constants** — declared at package scope:

```go
const (
    planMaxTokensCeiling     = 16384
    generateMaxTokensCeiling = 16384
    scopesMaxTokensCeiling   = 16384
    detectMaxTokensCeiling   = 4096
)
```

**`callLLM` signature** — gains `maxTokensCeiling`:

```go
func (c *Client) callLLM(
    ctx context.Context,
    system, user string,
    maxTokens, maxTokensCeiling int,
) (string, error)
```

The four call sites pass their endpoint's ceiling:

- `Generate` (~line 363): `c.callLLM(ctx, sys, user, 4096, generateMaxTokensCeiling)`
- `Plan` (~line 420): `c.callLLM(ctx, sys, user, 8192, planMaxTokensCeiling)`
- `DetectTechnologies` (~line 461): `c.callLLM(ctx, sys, user, 1024, detectMaxTokensCeiling)`
- `GenerateScopes` (~line 491): `c.callLLM(ctx, sys, user, 8192, scopesMaxTokensCeiling)`

**Heartbeat + per-attempt deadline** — wrap each `CreateChatCompletion`
call:

```go
for attempt := 0; attempt < maxAttempts; attempt++ {
    attemptCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
    done := make(chan struct{})
    go c.heartbeat(attemptCtx, done)

    resp, err := c.inner.CreateChatCompletion(attemptCtx, req)
    close(done)
    cancel()

    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) &&
            !errors.Is(ctx.Err(), context.Canceled) {
            lastErr = fmt.Errorf("request timed out after %s (model=%s, attempt=%d/%d)",
                c.requestTimeout, c.model, attempt+1, maxAttempts)
            continue
        }
        if errors.Is(err, context.Canceled) {
            return "", err // propagate SIGINT cleanly
        }
        if apiErr := classifyAPIError(err); apiErr != nil {
            return "", apiErr
        }
        lastErr = fmt.Errorf("openai chat completion: %w", err)
        continue
    }
    // ... existing finish_reason and content handling ...
}
```

`heartbeat` is a private method on `Client`:

```go
func (c *Client) heartbeat(ctx context.Context, done <-chan struct{}) {
    if c.out == nil {
        return
    }
    t := time.NewTicker(c.heartbeatInterval)
    defer t.Stop()
    start := time.Now()
    for {
        select {
        case <-done:
            return
        case <-ctx.Done():
            return
        case <-t.C:
            fmt.Fprintf(c.out, "still waiting on LLM... (%ds elapsed, model=%s)\n",
                int(time.Since(start).Seconds()), c.model)
        }
    }
}
```

**`FinishReasonLength` branch** — ceiling-aware:

```go
if len(resp.Choices) > 0 &&
    resp.Choices[0].FinishReason == goopenai.FinishReasonLength {
    next := req.MaxCompletionTokens * 2
    if next > maxTokensCeiling {
        return "", &commit.PlannerBudgetExhaustedError{
            Model:   c.model,
            Ceiling: maxTokensCeiling,
        }
    }
    req.MaxCompletionTokens = next
    lastErr = fmt.Errorf("LLM exhausted token limit at %d (model=%s, attempt=%d/%d)",
        req.MaxCompletionTokens/2, c.model, attempt+1, maxAttempts)
    continue
}
```

### 2.4 `infrastructure/git/client.go`

Add one method next to `StagedDiff`:

```go
func (c *Client) StagedDiffStat(ctx context.Context) (string, error) {
    out, err := exec.CommandContext(ctx, "git", "diff",
        "--staged", "--stat", "--ignore-submodules=all").Output()
    if err != nil {
        return "", err
    }
    return string(out), nil
}
```

### 2.5 `infrastructure/config/`

**`keys.go`** — register two user-scope keys and one project-scope key:

```go
"request_timeout":     {Name: "request_timeout",     Type: "duration", AllowUser: true},
"heartbeat_interval": {Name: "heartbeat_interval", Type: "duration", AllowUser: true},
"plan_fallback":      {Name: "plan_fallback",      Type: "string",   AllowProject: true, AllowLocal: true},
```

Add kebab aliases: `"request-timeout"`, `"heartbeat-interval"`,
`"plan-fallback"`.

**`resolver.go`** — extend `ProviderConfig`:

```go
type ProviderConfig struct {
    // ... existing fields ...
    RequestTimeout    time.Duration
    HeartbeatInterval time.Duration
}

const (
    DefaultRequestTimeout    = 90 * time.Second
    DefaultHeartbeatInterval = 15 * time.Second
)
```

Resolve via the existing precedence chain (flag → user config → default).

**`project.go`** — extend `project.Config`:

```go
type Config struct {
    // ... existing fields ...
    PlanFallback string `yaml:"plan_fallback,omitempty"` // "none" | "heuristic"; default "none"
}
```

### 2.6 `domain/commit/`

**`errors.go`** (new file):

```go
package commit

import "errors"

// ErrPlannerBudgetExhausted is the sentinel returned (wrapped) by the
// infrastructure LLM client when token doubling would exceed the
// per-endpoint ceiling. Use errors.Is to test for it.
var ErrPlannerBudgetExhausted = errors.New("planner budget exhausted")

// PlannerBudgetExhaustedError carries the model name and ceiling that
// were hit, so the cmd layer can render an actionable message.
type PlannerBudgetExhaustedError struct {
    Model   string
    Ceiling int
}

func (e *PlannerBudgetExhaustedError) Error() string {
    return ErrPlannerBudgetExhausted.Error()
}

func (e *PlannerBudgetExhaustedError) Is(target error) bool {
    return target == ErrPlannerBudgetExhausted
}
```

**`heuristic_planner.go`** (new file):

```go
package commit

import "context"

// HeuristicPlanner is a deterministic fallback planner used when the
// LLM planner returns ErrPlannerBudgetExhausted and the project has
// opted into plan_fallback: heuristic. Implementations must produce a
// non-empty CommitPlan when given a non-empty file list.
type HeuristicPlanner interface {
    Plan(ctx context.Context, req PlanRequest) (*CommitPlan, error)
}
```

Default implementation `directoryBucketer` lives in
`application/heuristic_planner.go` (it depends on `path.Dir` and
optionally on `project.Config.Scopes` — keeping it in application
avoids adding the `path` import to domain). Bucketing rule:

1. Group files by their first path component (`strings.SplitN(file, "/", 2)[0]`).
2. When `project.Config.Scopes` is non-empty, map each top-level
   directory to the scope whose description contains that directory
   name; unmapped directories fall back to scope `""`.
3. Cap at `maxCommitGroups` (5) — merge the smallest groups into the
   last one to stay under the cap.
4. Title format: `"chore(<scope>): update N files in <dir>/"` when
   scoped, `"chore: update N files in <dir>/"` otherwise. The
   per-group `Generate` call still refines the message later, so
   this title is only the planning placeholder.

### 2.7 `e2e/helpers_test.go`

Add a fake LLM server helper:

```go
func newFakeLLMServer(t *testing.T) *fakeLLMServer { /* ... */ }
```

Plus three handler factories (`stallHandler`, `slowHandler`,
`lengthHandler`) matching the test-only HTTP helpers section of
`bdd-specs.md`. No existing e2e test needs to change.

## 3. Dependency directions

All new dependencies point inward or stay within their layer.

| New code | Imports | Direction |
|---|---|---|
| `main.go` | `cmd`, stdlib `os/signal` | cmd → cmd (composition root) |
| `cmd/root.go.ExecuteContext` | stdlib `context`, Cobra | cmd → cmd |
| `cmd/commit.go` budget-error arm | `domain/commit` (for `*PlannerBudgetExhaustedError`) | cmd → domain (allowed) |
| `application/commit_service.go` synopsis | stdlib `strings`; uses `CommitGitClient.StagedDiffStat` | application → domain via interface (allowed) |
| `application/commit_service.go` fallback | `domain/commit`, `domain/project` (existing) | application → domain (allowed) |
| `application/heuristic_planner.go` (directoryBucketer) | stdlib `path`, `domain/commit`, `domain/diff`, `domain/project` | application → domain (allowed) |
| `infrastructure/openai/client.go` | stdlib `net/http`, `time`, `domain/commit` (for error type) | infrastructure → domain (allowed) |
| `infrastructure/git/client.go` `StagedDiffStat` | stdlib `os/exec`, `context` | infrastructure → infrastructure |
| `infrastructure/config/` | stdlib `time`; no domain coupling | infrastructure → stdlib |
| `domain/commit/errors.go`, `heuristic_planner.go` | stdlib `context`, `errors` | domain → stdlib only |

The domain layer remains free of external imports. The application layer
talks to infrastructure only through interfaces defined in itself
(`CommitGitClient`) or in domain (`HeuristicPlanner`,
`PlannerBudgetExhaustedError`).

## 4. Data structures

### 4.1 `DIFF-SYNOPSIS` block format

The synopsis is plain text with a hard-uppercase token on line 1 so the
LLM and any downstream tooling can detect it. The format:

```
DIFF-SYNOPSIS
file: <path>
changes: +<adds> / -<dels> (stat)
note: full diff elided (<actualBytes> bytes exceeded <cap>-byte cap)
```

Optional fifth section: the first 200 lines of the actual diff, prefixed
by `--- first 200 lines ---`. Included when the head-chopped chunk
contains valid line boundaries. Excluded for binary or vendored blobs
where the head is meaningless. Decision rule: include the tail when the
chunk has at least 10 newlines in its first 4 KiB; exclude otherwise.

### 4.2 Heartbeat line format

```
still waiting on LLM... (Ns elapsed, model=<model-id>)
```

Plain stderr, no carriage returns, no ANSI sequences — keeps CI logs
readable. One line per tick.

### 4.3 Phase line format

```
planning commits...
planned N commit(s)
commit i/N: drafting message (attempt j/k)
commit i/N: DIFF-SYNOPSIS fallback (<path>)
commit i/N: truncating group diff (Y bytes)
planner exhausted budget — falling back to directoryBucketer
```

Lower-case kebab-prose; each line stands alone; no per-file diff content
appears in any line.

## 5. Integration points and migration

### 5.1 Constructor change ripple

`infraOpenAI.NewClient` gains three parameters. Affected call sites:

- `cmd/commit.go:119` — pass resolved timeout, heartbeat, and writer.
- Existing unit tests that construct `Client` directly — add the three
  arguments. Most tests will pass `0, 0, nil` to inherit defaults and
  silence heartbeat output.

### 5.2 `CommitGitClient` interface change

Every fake / mock implementing this interface must add
`StagedDiffStat(ctx) (string, error)`. Affected:

- `application/commit_service_test.go` fakes
- `application/error_handling_test.go` fakes
- `application/verbose_test.go` fakes
- `application/add_service_test.go` fakes

Each fake gets a 4-line stub returning `"", nil` (or a canned string
when the test exercises REQ-007).

### 5.3 `CommitService` constructor change

Add `heuristicPlanner commit.HeuristicPlanner` as a new last parameter.
Test sites pass `nil`. The cmd-layer construction at
`cmd/commit.go:126-134` passes `application.NewDirectoryBucketer()` when
`projCfg.PlanFallback == "heuristic"`, otherwise `nil`.

### 5.4 Config rollout

`request_timeout` and `heartbeat_interval` default sensibly when absent
from user config — no user action required to benefit from REQ-001 and
REQ-004. `plan_fallback` defaults to `none` so REQ-008 is opt-in and the
default UX is unchanged.

## 6. Layer-by-layer summary

| Layer | New types | New functions | New files |
|---|---|---|---|
| `domain/commit/` | `ErrPlannerBudgetExhausted`, `PlannerBudgetExhaustedError`, `HeuristicPlanner` | — | `errors.go`, `heuristic_planner.go` |
| `domain/project/` | — | — | — (extend existing `Config` struct only) |
| `application/` | `directoryBucketer` (private) | `buildSynopsis`, `NewDirectoryBucketer` | `heuristic_planner.go` |
| `infrastructure/openai/` | — | `(*Client).heartbeat` | — |
| `infrastructure/git/` | — | `(*Client).StagedDiffStat` | — |
| `infrastructure/config/` | — | — | — (extend existing `keys.go`, `resolver.go`, `project.go`) |
| `cmd/` | — | `ExecuteContext` | — |
| `main` | — | (rewrite `main`) | — |
| `e2e/` | `fakeLLMServer` | three handler factories | — (extend `helpers_test.go`) |

Total new files: 3. Total new domain types: 3. Total new interface
methods: 2 (`StagedDiffStat`, `HeuristicPlanner.Plan`).
