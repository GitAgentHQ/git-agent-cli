# Best Practices

Security, performance, code quality, and lifecycle disciplines for the
large-diff "stuck" remediation. Each item below is a non-negotiable
constraint at implementation time.

## 1. Security

### 1.1 No secret material in stderr

The heartbeat goroutine and the promoted phase lines must emit only
metadata: model identifier, attempt counter, elapsed seconds, file
count, group index. They must never include:

- The API key (`providerCfg.APIKey`)
- The base URL host or path (`providerCfg.BaseURL`) — even a host name
  in stderr can leak the user's choice of provider to a screen-recorded
  bug report
- The system prompt or user prompt body
- The LLM response body, partial or full
- The assembled commit message before it has been committed
- File contents from `genDiff.Content` or `groupDiff.Content`

The discipline carries over from the existing `s.vlog` sites, which
already emit only metadata. Reviewers must check each new `s.out` call
against this list.

### 1.2 `DIFF-SYNOPSIS` derives from `git diff --stat`, not raw diff

`buildSynopsis` reads only the output of `git diff --staged --stat`
plus the file path and byte counters — not the truncated diff content.
A vendored or minified file may embed secrets in its source; the
synopsis path is the safer prompt when that file's diff alone saturates
the byte cap, *because* it avoids sending the file body. When the
optional "first 200 lines" tail is included (per `architecture.md` §4.1
decision rule), it is included only when the chunk has ≥10 newlines in
its first 4 KiB, which excludes vendored / minified blobs by
construction.

### 1.3 Error messages do not leak prompts or responses

`PlannerBudgetExhaustedError.Error()` returns only the sentinel string;
the cmd-layer rendering adds model name and ceiling. Neither layer
embeds the prompt that was sent or the partial response that was
received.

### 1.4 Signal-handler scope

`signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` is scoped to
the root command and released via `defer stop()`. It does not register
for SIGKILL (which is uncatchable) or SIGHUP (which has different
semantics — daemons reload config on SIGHUP, CLIs typically should not
catch it).

## 2. Performance

### 2.1 Heartbeat goroutine lifecycle

The heartbeat goroutine is spawned per `CreateChatCompletion` attempt
inside `callLLM`. It exits via the first of three signals:

1. `done` channel close — the LLM call returned (success or error).
2. `ctx.Done()` — the parent context was cancelled (SIGINT or per-attempt
   deadline).
3. `ticker.C` fired AND the next loop iteration finds case 1 or 2.

Cleanup invariants:

- `defer ticker.Stop()` releases the timer immediately on return.
- The `done` channel is closed exactly once via `close(done)` after
  `CreateChatCompletion` returns; the goroutine selects on
  `<-done`, not on a buffered channel that could leak.
- A `goleak.VerifyTestMain(m)` line is added to
  `infrastructure/openai/client_test.go` to fail any test that leaves
  the heartbeat alive.

### 2.2 No background goroutines outside the LLM adapter

The application and domain layers stay synchronous. The only goroutine
introduced is the heartbeat, owned by `infrastructure/openai/client.go`.
Concurrency in `infrastructure/git/client.go` (existing parallel
`exec.CommandContext` patterns at lines 28-46 and 217-239) is unchanged.

### 2.3 HTTP client reuse

`*http.Client` is constructed once in `NewClient` and reused for every
call on a single `*Client`. `Timeout` on the client applies per request
(Go stdlib semantics), so per-attempt deadlines are correctly bounded
without per-attempt client allocation. A child
`context.WithTimeout(ctx, c.requestTimeout)` per attempt enforces the
same deadline at the context level so the SDK propagates cancellation
into the request goroutine even if the SDK does not call back into
`http.Client.Timeout`.

### 2.4 Heartbeat noise threshold

15 s default keeps quick calls (≤14 s) entirely silent. The interval is
configurable via `heartbeat_interval`. The first tick fires at
`heartbeat_interval`, not at second zero, so a 5 s call produces zero
ticks. This was a deliberate trade-off versus the alternative "first
tick after 10 s, then every 15 s" — added complexity for marginal
quieting; rejected.

### 2.5 Token ceiling avoids strictly-worse failure mode

Token doubling makes the failure mode *strictly slower* when the
underlying issue is a misbehaving model returning chain-of-thought
that never lands in the JSON envelope. For the Plan endpoint, the
correct response is small JSON; if a model cannot fit it in 8192 tokens,
doubling to 16384 and then 32768 will not help — it adds two slow
round-trips and ends with the same error. The 16384 ceiling caps the
wasted wall-clock time at ~1 retry past the seed.

## 3. Code quality

### 3.1 Clean Architecture preservation

- `domain/commit/errors.go` and `domain/commit/heuristic_planner.go`
  import only stdlib (`context`, `errors`). No external imports.
- `application/commit_service.go` reaches outward only through
  interfaces defined in itself (`CommitGitClient`) or in domain
  (`commit.HeuristicPlanner`, `commit.CommitMessageGenerator`,
  `commit.CommitPlanner`). It does not import `infrastructure/...`.
- `application/heuristic_planner.go` (directoryBucketer) imports
  `path`, `domain/commit`, `domain/diff`, `domain/project`. No
  infrastructure imports.
- `infrastructure/openai/client.go` imports `domain/commit` for the
  typed error — outer-to-inner dependency, allowed.
- `cmd/commit.go` imports `domain/commit` for the
  `*PlannerBudgetExhaustedError` switch arm — outer-to-inner, allowed.

### 3.2 Naming consistency

Per the Glossary in `_index.md`:

- Config keys are snake_case (`request_timeout`,
  `heartbeat_interval`, `plan_fallback`).
- CLI flags are kebab-case mirrors (`--request-timeout`,
  `--heartbeat-interval`, `--plan-fallback`).
- Go constants are CamelCase (`DefaultRequestTimeout`,
  `planMaxTokensCeiling`).
- Interfaces use the noun-only form (`HeuristicPlanner`, not
  `HeuristicPlannerService`); concrete implementations carry a
  qualifier (`directoryBucketer`).

### 3.3 Error wrapping discipline

- `commit.ErrPlannerBudgetExhausted` is the sentinel for `errors.Is`.
- `commit.PlannerBudgetExhaustedError` is the carrier for `errors.As`
  in the cmd layer.
- The application layer wraps with `fmt.Errorf("plan commits: %w", err)`,
  preserving the typed error.
- The cmd layer uses `errors.As(err, &budgetErr)` to extract the
  carrier and render an actionable message.
- Existing `*agentErrors.APIError` and `*agentErrors.ExitCodeError`
  handling is unchanged.

### 3.4 Test isolation

- Unit tests use `httptest.NewServer` for fake LLM endpoints; no
  external network calls.
- E2E tests rely on the existing subprocess pattern in
  `e2e/helpers_test.go`; the new `newFakeLLMServer` helper is the
  fake transport.
- `goleak.VerifyTestMain` runs in `infrastructure/openai` to catch
  any heartbeat goroutine leaks.
- No `time.Sleep` calls in tests for the heartbeat — inject a fake
  ticker channel or use `time.NewTimer` with a controllable clock.

### 3.5 Existing convention compliance

- 2-space YAML indentation in config files (matches existing
  `.git-agent/config.yml` layout).
- Conventional commit messages, lowercase title ≤50 chars, scopes
  from the project allow-list (`docs`, `plans`, `design`, `cli`,
  plus whatever the auto-scoper adds for `app`, `infra`).
- No emojis in code, comments, error messages, or this design.

## 4. Common pitfalls

### 4.1 Confusing `context.DeadlineExceeded` with `context.Canceled`

The two errors require different handling and produce different stderr
output:

- `context.DeadlineExceeded` — per-attempt HTTP timeout fired. The
  retry loop should fire the next attempt. The stderr message reads
  `request timed out after Xs (model=..., attempt=j/k)`.
- `context.Canceled` — SIGINT received. The retry loop must NOT fire
  the next attempt. The error must propagate cleanly to the cmd layer
  which prints `cancelled` and exits non-zero.

The implementation check (`architecture.md` §2.3) distinguishes by
inspecting `errors.Is(ctx.Err(), context.Canceled)` only when the
attempt's own deadline fired.

### 4.2 Double-printing on `--verbose`

If a `vlog` site is promoted to `out` *and* the original `vlog` call is
left in place, the verbose stream will contain the line twice. Each
promotion in `architecture.md` §2.2 replaces the `s.vlog` call, not
duplicates it.

### 4.3 Truncating a `--stat` line at a byte boundary

`StagedDiffStat` output is small (one line per file plus a totals
footer). It is not subject to the byte cap. Do not pass it through the
truncator.

### 4.4 Re-staging race on SIGINT

The `defer` at `application/commit_service.go:347-361` uses
`context.Background()` for the recovery `StageFiles` call — intentional,
because the outer context is cancelled. After the redesign this
invariant must hold: the recovery context must not be derived from the
cancelled context, or the re-stage will fail too.

### 4.5 Heartbeat fighting a future TTY spinner

The heartbeat emits plain newline-terminated lines with no carriage
returns and no ANSI sequences. If a future change introduces an ANSI
spinner, that spinner must detect `!isatty(stderr)` and degrade to the
heartbeat format; the heartbeat itself stays line-mode unconditionally.

### 4.6 `directoryBucketer` mis-routing files

The scope-name → directory match (`architecture.md` §2.6) is best-effort.
A doc change that touches a file under `infrastructure/` will end up in
the `infra` group, not a hypothetical `docs` group. The fallback is
opt-in (`plan_fallback: heuristic`) precisely because of this — users
who care about precise scope assignment leave the default and accept
the hard-error on planner exhaustion.

### 4.7 Ceiling raises a new visible failure mode

Today's "infinite doubling" eventually returns a truncated response and
the JSON parser fails with a parse error. The new ceiling-based path
fails earlier with `PlannerBudgetExhaustedError`. Reviewers must verify
that the actionable error message (REQ-006) is visible and that no test
asserts on the old `max_tokens=32768, attempt=3/3` substring (e2e tests
should be updated to assert on the new format).

### 4.8 Heartbeat and per-attempt timeout interaction

A `heartbeat_interval` longer than `request_timeout` produces zero ticks
per attempt. This is documented behaviour, not a bug — users wanting
ticks must set `heartbeat_interval < request_timeout`. The default
values (15 s and 90 s) satisfy this for the default flow.

## 5. Deferred refactors

The following refactors were considered during design and explicitly
deferred. They are not part of the v1 scope and must not be back-doored
in during implementation.

### 5.1 `ProgressSink` interface + typed `Event` values

The BDD sub-agent proposed unifying `s.out` and `s.vlog` behind a
single `ProgressSink` interface with `PhaseStartEvent`, `RetryEvent`,
`HeartbeatEvent`, `DetailEvent` value types. Long-term this is cleaner
(single emit point, severity filter in cmd layer, JSON-output friendly).
For v1 the current two-channel split already supports the locked
behaviour with no new types. Per the project's "Match complexity to
actual scale" rule, this is YAGNI now. Revisit when a third sink
(e.g., a structured-log adapter for CI consumption) is required.

### 5.2 Streaming LLM responses

`go-openai` v1.41.2 supports `CreateChatCompletionStream`. Adopting it
would collapse the "is it alive?" question into first-token latency,
making the heartbeat largely redundant. Bigger code change, the proxy
may not pass streaming end-to-end (model-name masking is harder while
streaming), and the heartbeat goroutine here already addresses the
signal-of-life problem at a fraction of the complexity. Defer until a
specific user need for streaming surfaces.

### 5.3 Parallel per-group `Generate` calls

Wall-clock win on multi-group commits, but breaks the sequential
`UnstageAll → StageFiles → StagedDiff` invariant in
`CommitService.Commit` and risks index races. The git index is
single-writer; serialising on it would require an in-process lock and
careful per-group worktree snapshots. High blast radius for a UX fix.
Defer.

### 5.4 Replacing `github.com/sashabaranov/go-openai`

Considered for tighter HTTP control. Rejected: the SDK already exposes
`ClientConfig.HTTPClient` (verified at v1.41.2), and replacing it
introduces a substantial maintenance burden for marginal gain.
