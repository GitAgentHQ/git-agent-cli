# Design: large-diff "stuck" remediation

Make `git-agent commit` survive large-diff and slow-LLM scenarios without
appearing to hang. Address the user-reported symptom `在修改量特别大的情况下，
git-agent 会被卡住` by removing the four mechanisms that compound to produce
the "stuck" experience: missing HTTP timeout, missing signal handling,
silent default-mode output, and unbounded token-doubling retries.

## Context

### User-visible problem

When a user runs `git-agent commit` against a very large staged change set,
the binary appears to hang for minutes with no output, and may exit with a
bare error such as:

```
Error: plan commits: LLM exhausted token limit
       (model=deepseek-v4-flash, max_tokens=32768, attempt=3/3)
```

There is no progress indication during the wait, no way to interrupt the
in-flight LLM call other than killing the shell, and the final error tells
the user *what* failed but not *what to do next*.

### Scope

This design is scoped to the `git-agent-cli` Go binary. It does not change
the proxy (`git-agent-proxy/`) or the dashboard (`git-agent-home/`). It
preserves the existing Clean Architecture boundaries (cmd → application →
domain ← infrastructure) and reuses the already-shipped `max_diff_bytes`
work (commits af9951b…d28b4bb) as the request-body cap.

### Out of scope

- Streaming LLM responses (would replace total-latency with first-token
  latency for the "is it alive?" question; bigger code change, proxy may
  not stream end-to-end). Deferred.
- Parallelising per-group `Generate` calls (breaks the sequential
  `UnstageAll → StageFiles → StagedDiff` invariant; risks index races).
  Deferred.
- A `ProgressSink` interface with typed `Event` values as a single sink for
  both always-on and verbose output. The current `s.out` / `s.vlog` split
  already supports the locked behaviour; introducing a sink + event-type
  abstraction is YAGNI for v1. Marked as a deferred refactor in the
  best-practices doc.
- Replacing the `github.com/sashabaranov/go-openai` client with a
  hand-rolled HTTP client.

## Discovery Results

All references are to files under `git-agent-cli/`.

1. **No HTTP timeout on the OpenAI client.** `infrastructure/openai/client.go:24-33`
   builds the SDK client via `goopenai.DefaultConfig(apiKey)` and
   `goopenai.NewClientWithConfig(cfg)`. No `*http.Client` is injected, so
   the SDK falls back to its zero-value `&http.Client{}` which has no
   `Timeout`. Any upstream stall (proxy cold-start, AI Gateway queue,
   dropped TLS) hangs the binary indefinitely. The SDK *does* honour
   `ctx` cancellation through `CreateChatCompletion(ctx, req)` at
   `client.go:219`, so the missing piece is a timeout that produces a
   cancellation, not a missing cancellation path.

2. **No signal handling.** `main.go:5-7` calls `cmd.Execute()` which calls
   `rootCmd.Execute()` (`cmd/root.go:23-31`) — Cobra's no-context form.
   `cmd.Context()` inside `runCommit` therefore resolves to
   `context.Background()`. A grep for `signal.Notify`, `signal.NotifyContext`,
   `os/signal`, or `Interrupt` across the codebase returns nothing. SIGINT
   from the shell kills the process directly via the OS, bypassing the
   defer at `application/commit_service.go:347-361` that would otherwise
   re-stage uncommitted files on error.

3. **Default-mode silence.** Every phase log in the commit pipeline is
   gated by `req.Verbose`. `application/commit_service.go:104-108`:

   ```go
   func (s *CommitService) vlog(req CommitRequest, format string, args ...any) {
       if req.Verbose && req.LogWriter != nil {
           fmt.Fprintf(req.LogWriter, format+"\n", args...)
       }
   }
   ```

   The always-on `s.out` helper at lines 110-114 exists but is currently
   used only for hook-rejection feedback. In default mode the user sees
   nothing between invocation and the first commit hash — for a large
   diff that is one to five minutes of dark.

4. **Token-doubling retry loop with no per-endpoint ceiling.**
   `infrastructure/openai/client.go:233-238` doubles `MaxCompletionTokens`
   on every `finish_reason=length`:

   ```go
   if len(resp.Choices) > 0 &&
       resp.Choices[0].FinishReason == goopenai.FinishReasonLength {
       req.MaxCompletionTokens *= 2
       lastErr = fmt.Errorf("LLM exhausted token limit (model=%s, max_tokens=%d, attempt=%d/%d)",
           c.model, req.MaxCompletionTokens/2, attempt+1, maxAttempts)
       continue
   }
   ```

   Plan starts at 8192 (`client.go:420`) → 16384 → 32768. Generate starts
   at 4096 (`client.go:363`) → 8192 → 16384 → 32768. There is no
   per-endpoint ceiling. The observed `deepseek-v4-flash` failure trace
   exhausted all three retries at 32768 tokens, each a full network
   round-trip with no progress signal, then exited with no commit.

5. **LLM-call cascade on large diffs.** Worst case per invocation:
   1 scope-generation call (auto-scope when `Scopes` empty) → 1 plan call
   → optional 1 scope-refresh re-plan when groups lack scopes → for each
   of up to `maxCommitGroups=5` groups, up to `maxHookRetries=3` Generate
   attempts → up to `maxRePlans=2` re-plans on hook failure. Roughly 15
   sequential HTTP round-trips, each potentially 30 s on a slow upstream,
   all silent in default mode.

6. **Per-group truncator drops the tail.** `infrastructure/diff/truncator.go:31-34`
   simply slices content to `maxBytes`. The comment block at lines 47-57
   explicitly explains this is deliberate so a single oversized line is
   not discarded back to an early newline. The trade-off: when a group
   contains exactly one file whose own diff is over the byte cap, the
   LLM receives a head-chopped chunk with no signal that the rest is
   missing. The `--max-diff-bytes` cap defaults to 384 KiB to fit inside
   the proxy's 512 KiB request limit (`application/commit_service.go:120-128`),
   so this is a routine event on vendored or minified files.

7. **Filter strategy.** `pkg/filter/patterns.go:7-25` drops lock files
   (`package-lock.json`, `pnpm-lock.yaml`, etc.) and binary assets by
   basename before the truncator runs. `infrastructure/diff/filter.go:30-34`
   keeps the file *list* but strips the diff *content* — so the planner
   never sees lock-file diffs and the per-group truncator only operates
   on real source diffs.

8. **Prior work on diff size.** Commits `af9951b`, `f738ec5`, `e983a29`,
   `3298e52`, `d28b4bb` introduced `--max-diff-bytes` and the byte cap.
   This bounds the *request body*, which prevented one class of
   "stuck" symptom (proxy rejects oversized request, retry loop spins).
   It does not bound *response time* or *call count*, which are the
   remaining causes of the symptom this design addresses.

## Glossary

Canonical labels used across all four design files. Rejected variants are
recorded so future readers see what was considered.

| Concept | Canonical label | Rejected variants |
|---|---|---|
| Per-attempt HTTP deadline | `request_timeout` (config key, duration string), `--request-timeout` (flag) | `llm_timeout`, `http_timeout`, `request-timeout-seconds` |
| Per-endpoint token doubling cap | `planMaxTokensCeiling`, `generateMaxTokensCeiling`, `scopesMaxTokensCeiling`, `detectMaxTokensCeiling` (constants in `infrastructure/openai/`) | `planner_max_tokens` (project YAML key — too granular for v1) |
| High-level user-visible phase output | promote selected `s.vlog` sites to `s.out` (always-on stderr) | new `ProgressSink` interface + typed `Event` values |
| Live-wait progress signal during LLM call | inline heartbeat goroutine inside `callLLM`, emitting one stderr line every 15 s | domain-level `HeartbeatEvent`; ANSI spinner |
| Per-group oversized-single-file fallback content | `DIFF-SYNOPSIS` block (uppercase token + four lines: `file:`, `changes:`, `note:`, optional first-200-lines tail) | plain `[diff omitted: …]` prefix |
| Git method providing the synopsis source | `CommitGitClient.StagedDiffStat(ctx) (string, error)` | `DiffStat(ctx, files)` |
| Typed error on token-budget exhaustion | `commit.ErrPlannerBudgetExhausted` sentinel + `commit.PlannerBudgetExhaustedError` carrier | wrapped `fmt.Errorf` string only |
| Opt-in non-LLM planner fallback | `commit.HeuristicPlanner` (interface), `directoryBucketer` (default impl), `plan_fallback: none\|heuristic` (config key, default `none`) | `DirectoryBucketer` as the public interface name; `--no-planner` (flag-only, no persistence) |
| Top-level signal-aware context | `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` in `main.go`, wired via `rootCmd.ExecuteContext(ctx)` | `signals.SetupSignalHandler`-style helper package |

## Requirements

### Functional

- **REQ-001 Per-attempt HTTP timeout.** Every LLM HTTP request runs under
  a child context derived from the caller's context with a deadline
  configured by `request_timeout` (default `90s`, user-scope only).
  Timeout-on-attempt triggers the existing retry loop in `callLLM`.

- **REQ-002 Process-level SIGINT/SIGTERM cancellation.** `main.go` wires
  `signal.NotifyContext` and `rootCmd.ExecuteContext(ctx)` so that
  Ctrl-C from the shell cancels the root context. The cancellation
  reaches the in-flight HTTP call within the per-attempt timeout. The
  existing `defer` at `application/commit_service.go:347-361` re-stages
  uncommitted files via `context.Background()` so cancellation cleanup
  succeeds.

- **REQ-003 Always-on phase output.** In default (non-verbose) mode, the
  user sees a stderr line for each of these milestones: auto-scope
  start, plan start, plan group count, scope-refresh start, per-group
  "commit i/N: drafting (attempt j/k)", hook retry, and (when it
  triggers) `DIFF-SYNOPSIS` fallback. Verbose output remains a strict
  superset — every always-on line also appears under `--verbose`, with
  additional detail lines interleaved; no line is duplicated.

- **REQ-004 Heartbeat tick.** Each LLM HTTP call started inside `callLLM`
  spawns a heartbeat goroutine that emits one stderr line every
  `heartbeat_interval` (default `15s`) while the call is in flight,
  naming elapsed seconds and the model. The goroutine exits within
  100 ms of the call returning (success, error, or cancellation).

- **REQ-005 Per-endpoint token-doubling ceiling.** `callLLM` accepts a
  ceiling argument and refuses to double `MaxCompletionTokens` past it.
  Per-endpoint ceilings: `planMaxTokensCeiling=16384`,
  `generateMaxTokensCeiling=16384`, `scopesMaxTokensCeiling=16384`,
  `detectMaxTokensCeiling=4096`. When the next double would exceed the
  ceiling, `callLLM` returns a typed
  `commit.PlannerBudgetExhaustedError` instead of issuing another
  attempt.

- **REQ-006 Actionable error on budget exhaustion.** The error surfaced
  to the user on `commit.PlannerBudgetExhaustedError` names the model
  in use, the ceiling that was hit, and at least two concrete
  remediations chosen from: `--max-diff-lines`, `--max-diff-bytes`,
  `--intent`, "try a more capable model", "commit smaller batches".
  Example: `LLM kept producing oversized output (model=deepseek-v4-flash,
  ceiling=16384 tokens); try a more capable model, narrow scope with
  --intent, or split with --max-diff-lines / smaller batches`.

- **REQ-007 Per-group `DIFF-SYNOPSIS` fallback.** When the per-group
  truncator path triggers AND the group contains exactly one file AND
  the truncated content length equals the byte cap (meaning the
  single-file diff alone saturated the cap), the per-group block in
  `CommitService.Commit` replaces `genDiff.Content` with a
  `DIFF-SYNOPSIS` block sourced from
  `CommitGitClient.StagedDiffStat(ctx)`. The LLM still produces a
  commit message; the hook still receives the full raw diff.

- **REQ-008 Opt-in heuristic planner fallback.** When the LLM planner
  returns `commit.ErrPlannerBudgetExhausted` and `project.Config.PlanFallback
  == "heuristic"`, `CommitService` invokes a deterministic
  `directoryBucketer` that groups files by top-level directory (matched
  to `Config.Scopes` when possible). Default value of `plan_fallback`
  is `none`, in which case the existing hard-error path is preserved.

### Non-functional

- **REQ-009 Backward compatibility.** `go test ./...` passes unchanged
  after the design lands. Existing `e2e/helpers_test.go` subprocess
  invocations continue to work; the signal handler in `main.go` is a
  no-op when the parent process does not deliver a signal.

- **REQ-010 Verbose superset.** `--verbose` output strictly contains
  every always-on line plus additional detail lines, with no
  duplication. A diff of the two stderr streams from the same input
  must show only additions in the verbose run.

- **REQ-011 No secret leakage in progress output.** Heartbeat and phase
  lines emit only metadata (model name, attempt counter, elapsed
  seconds, file count, group index). They never include the API key,
  base URL, prompt content, response content, or assembled commit
  message.

## Rationale

The "stuck" symptom is the user-visible composite of four interacting
mechanisms, all of which become acute on large diffs:

1. **No HTTP timeout** lets a single stalled request hang indefinitely.
   The Go default `&http.Client{}` has zero `Timeout`; the SDK accepts a
   client via `ClientConfig.HTTPClient` of type `HTTPDoer` which
   `*http.Client` satisfies directly. Adding the timeout is a one-line
   change at the construction site.

2. **No signal handler** means the user's only recovery is to kill the
   shell, which bypasses the existing `defer`-based re-stage path. A
   process-level `signal.NotifyContext` propagates the cancellation
   through every `cmd.Context()` consumer without per-call-site changes.

3. **Default-mode silence** turns a working pipeline into an
   indistinguishable-from-stuck experience. Promoting roughly eight of
   the existing `s.vlog` sites to `s.out` is sufficient — no new
   abstraction is required to observe the milestones the user actually
   needs.

4. **Unbounded token doubling** makes the failure mode strictly worse on
   misbehaving models. The observed `deepseek-v4-flash` trace exhausted
   3 attempts at 32768 tokens for a Plan response that should fit in
   ~2000 tokens. A per-endpoint ceiling caps the wasted wall-clock time
   at ~1 retry, and a typed error gives the user a concrete next step.

The four mechanisms can be fixed independently and incrementally. They
share one surface — the `infrastructure/openai/client.go` `callLLM`
function — but the cmd-layer signal wiring and application-layer
synopsis fallback are isolated from each other.

**Alternatives considered**:

- *Streaming responses.* The user would see live tokens, which collapses
  the "is it alive?" question into first-token latency. Bigger code
  change, the proxy may not pass streaming end-to-end (the proxy masks
  model identity in the response, which is harder to do while
  streaming), and the heartbeat goroutine here already addresses the
  signal-of-life problem at a fraction of the complexity. Deferred.

- *Parallel per-group `Generate` calls.* Wall-clock win on multi-group
  commits, but breaks the sequential `UnstageAll → StageFiles →
  StagedDiff` invariant in `CommitService.Commit` and risks index
  races. High blast radius for a UX fix. Deferred.

- *Pure template-only fallback (no LLM at all).* Defeats the product.
  Rejected.

- *"Just raise `max_diff_bytes`."* Already user-tunable. The cap exists
  because the proxy enforces 512 KiB. Does not fix the no-timeout root
  cause.

- *A `ProgressSink` interface with typed `Event` values* (proposed by
  the BDD sub-agent). Cleaner long-term, but the current two-channel
  split (`s.out` + `s.vlog`) already supports the locked behaviour
  with no new types. Per the project's "Match complexity to actual
  scale" rule, this is YAGNI for v1. Captured as a deferred refactor
  in `best-practices.md`.

## Detailed Design

See `architecture.md` for the full component layout and `bdd-specs.md`
for executable scenarios. High-level summary:

1. **`main.go`** gains a four-line `signal.NotifyContext` wrapper and
   calls `rootCmd.ExecuteContext(ctx)`. `cmd/root.go` exposes
   `ExecuteContext(ctx)` alongside the existing `Execute()`.

2. **`infrastructure/config/`** adds the `request_timeout` and
   `heartbeat_interval` keys to the registry (`keys.go`) and to
   `ProviderConfig` (`resolver.go`); both are user-scope only because
   they couple to provider behaviour. `plan_fallback` goes on
   `project.Config` (project-scope).

3. **`infrastructure/openai/client.go`** changes:
   - `NewClient` signature gains `timeout time.Duration` and
     `out io.Writer` parameters; the timeout becomes
     `cfg.HTTPClient = &http.Client{Timeout: timeout}`; the writer is
     stored on `Client` for the heartbeat.
   - `callLLM` signature gains `maxTokensCeiling int`; the
     `FinishReasonLength` branch refuses to double past the ceiling
     and returns a typed `*commit.PlannerBudgetExhaustedError`.
   - `callLLM` opens a child `context.WithTimeout(ctx,
     c.requestTimeout)` per attempt and spawns the heartbeat
     goroutine.
   - Per-endpoint constants `planMaxTokensCeiling`,
     `generateMaxTokensCeiling`, `scopesMaxTokensCeiling`,
     `detectMaxTokensCeiling` are passed to `callLLM` from the four
     call sites.

4. **`infrastructure/git/client.go`** adds `StagedDiffStat(ctx)` which
   runs `git diff --staged --stat --ignore-submodules=all`.

5. **`application/commit_service.go`** changes:
   - `CommitGitClient` interface gains `StagedDiffStat(ctx) (string, error)`.
   - The per-group block (around lines 386-401) inserts the
     single-file saturation check after `s.truncator.Truncate` and
     swaps in the `DIFF-SYNOPSIS` block when it triggers.
   - The post-`Plan`-call error handling (lines 281-283 and 312-320)
     checks `errors.Is(err, commit.ErrPlannerBudgetExhausted)` and
     invokes `s.heuristicPlanner.Plan(ctx, req)` when the config
     opts in.
   - Eight existing `s.vlog` sites are promoted to `s.out` per the
     table in `architecture.md` §3.

6. **`domain/commit/`** adds `errors.go` (or extends an existing file)
   with `ErrPlannerBudgetExhausted` sentinel and
   `PlannerBudgetExhaustedError{Model, Ceiling}` carrier; adds
   `heuristic_planner.go` with the `HeuristicPlanner` interface (signature
   identical to `CommitPlanner`).

7. **`cmd/commit.go`** threads `request_timeout`, `heartbeat_interval`,
   and `plan_fallback` from resolved config into `NewClient` and into
   `CommitRequest`. Error-path handling adds a switch arm for
   `*commit.PlannerBudgetExhaustedError` that prints the actionable
   message and returns exit code 1.

## Design Documents

- [bdd-specs.md](bdd-specs.md) — Gherkin scenarios for every REQ-NNN
- [architecture.md](architecture.md) — components, data flow, interfaces, line-level changes
- [best-practices.md](best-practices.md) — security, performance, lifecycle, deferred refactors
