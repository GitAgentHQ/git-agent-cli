# BDD Specifications

Every scenario tags the REQ-NNN it covers. Given clauses use concrete data
values; no vague placeholders.

## Feature: Bounded LLM HTTP request with cancellation

```gherkin
Feature: Bounded LLM HTTP request with cancellation
  As a developer running git-agent commit
  I want every LLM round-trip to be time-bounded and Ctrl-C-cancellable
  So that git-agent never hangs indefinitely on a stalled network

  Background:
    Given a git repository at /tmp/large-diff-fixture with the commit history of cmd/root.go visible to git log
    And an API key string "sk-test-key-001" in ~/.config/git-agent/config.yml
    And the resolved request_timeout is 90 seconds
    And the heartbeat_interval is 15 seconds

  @REQ-001 @http-timeout
  Scenario: Per-attempt timeout fires three retries instead of hanging
    Given a fake LLM endpoint at http://127.0.0.1:18080 that accepts the TCP connection and writes no response body
    And the configured request_timeout is 5 seconds (lowered for the test)
    And the configured base_url is http://127.0.0.1:18080
    When I run "git-agent commit" against the fixture
    Then attempt 1 aborts after 5 seconds with context.DeadlineExceeded
    And attempt 2 aborts after 5 seconds with the same deadline
    And attempt 3 aborts after 5 seconds with the same deadline
    And the process exits within 17 seconds with exit code 1
    And stderr contains the literal substring "request timed out after 5s"
    And stderr names the model from the resolved config
    And stderr does not contain the strings "panic", "goroutine", or "context.DeadlineExceeded" raw

  @REQ-002 @cancellation
  Scenario: SIGINT during an LLM call cancels within one second and re-stages
    Given the user pre-staged the files "a.go" and "b.go" via "git add a.go b.go"
    And the working tree also contains an unstaged file "c.go"
    And a fake LLM endpoint at http://127.0.0.1:18080 that holds the response open for 600 seconds
    And the configured base_url is http://127.0.0.1:18080
    When I run "git-agent commit" and send SIGINT to its PID after the heartbeat tick at second 15 is observed
    Then the in-flight HTTP request is cancelled within 1 second of the signal
    And no commit is created
    And "git diff --staged --name-only" lists "a.go" and "b.go" exactly
    And "c.go" remains unstaged
    And the process exits with a non-zero exit code
    And stderr contains the literal substring "cancelled"
    And stderr does not contain a Go panic stack trace
```

## Feature: Always-on phase progress and heartbeat

```gherkin
Feature: Always-on phase progress and heartbeat
  As a developer running git-agent commit without --verbose
  I want a recognizable signal of life during multi-call LLM work
  So that I can distinguish a working pipeline from a hang

  Background:
    Given a git repository at /tmp/moderate-diff-fixture with 3 changed files totalling 40 KB of diff
    And the configured heartbeat_interval is 15 seconds
    And a fake LLM endpoint that returns a plan in 800 ms and per-group messages in 600 ms each

  @REQ-003 @progress
  Scenario: Default-mode commit prints recognizable phase lines on stderr
    Given the project config at .git-agent/config.yml declares 3 scopes
    And the planner returns 2 commit groups
    When I run "git-agent commit" without --verbose
    Then stderr contains the line matching the regex "^planning"
    And stderr contains the line "planned 2 commit(s)"
    And stderr contains a line matching "^commit 1/2: drafting message \(attempt 1/3\)"
    And stderr contains a line matching "^commit 2/2: drafting message \(attempt 1/3\)"
    And stderr contains no per-file diff content
    And stdout contains the 2 resulting commit hashes and explanations

  @REQ-004 @heartbeat
  Scenario: A 47-second LLM call emits two heartbeat ticks
    Given a fake LLM endpoint that holds the planner response open for 47 seconds before sending JSON
    And the configured heartbeat_interval is 15 seconds
    When I run "git-agent commit" against the fixture
    Then stderr contains exactly 2 lines matching "^still waiting on LLM... \([0-9]+s elapsed"
    And the first tick line reports elapsed seconds in the closed range [15, 17]
    And the second tick line reports elapsed seconds in the closed range [30, 32]
    And each tick line names the model from the resolved config
    And the heartbeat goroutine stops within 100 ms of the planner response completing
    And the test does not report a leaked goroutine via goleak

  @REQ-010 @verbose
  Scenario: --verbose output is a strict superset of always-on output
    Given the same fixture and fake endpoint as the default-mode scenario above
    When I run "git-agent commit --verbose" and capture stderr as stream V
    And I run "git-agent commit" without --verbose and capture stderr as stream A
    Then every line in stream A appears exactly once in stream V
    And every line in stream A appears at most once in stream A
    And stream V contains additional lines absent from stream A, including "staged files:" and "unstaged files:"
    And no line is duplicated within either stream
```

## Feature: Per-endpoint token-doubling ceiling

```gherkin
Feature: Per-endpoint token-doubling ceiling
  As a developer using a small "flash" model
  I want repeated finish_reason=length responses to fail fast with actionable guidance
  So that the binary stops wasting wall-clock time on a misbehaving model

  Background:
    Given the resolved model is "deepseek-v4-flash"
    And the configured planMaxTokensCeiling is 16384
    And the configured generateMaxTokensCeiling is 16384

  @REQ-005 @token-budget
  Scenario: Planner doubling halts at the ceiling after one attempted double
    Given a fake LLM endpoint that returns finish_reason=length on attempt 1 with max_completion_tokens=8192
    And the same fake returns finish_reason=length on attempt 2 with max_completion_tokens=16384
    When git-agent issues the planner call
    Then a third HTTP request to the planner endpoint is never sent
    And callLLM returns an error of type *commit.PlannerBudgetExhaustedError
    And the error carries Model "deepseek-v4-flash" and Ceiling 16384

  @REQ-006 @token-budget @diagnostic
  Scenario: Budget-exhausted error names the model and at least two remediations
    Given the planner has returned commit.ErrPlannerBudgetExhausted
    When the cmd layer renders the error to stderr
    Then stderr contains the literal substring "model=deepseek-v4-flash"
    And stderr contains the literal substring "ceiling=16384"
    And stderr contains at least two of the following remediation strings: "--max-diff-lines", "--max-diff-bytes", "--intent", "try a more capable model", "commit smaller batches"
    And the process exits with exit code 1
    And stderr does not contain the substring "max_tokens=32768" (no leftover from the old doubling path)
```

## Feature: Per-group fallback for oversized single-file diffs

```gherkin
Feature: Per-group fallback for oversized single-file diffs
  As a developer committing a vendored or minified file
  I want the LLM to receive a synopsis instead of a head-chopped diff
  So that the commit message reflects "large change to file X" rather than gibberish

  Background:
    Given the configured max_diff_bytes is 393216
    And the planner has produced 1 commit group whose Files is ["vendored/bundle.min.js"]

  @REQ-007 @synopsis
  Scenario: Single-file 1 MB diff triggers the DIFF-SYNOPSIS fallback
    Given the staged diff for "vendored/bundle.min.js" is 1048576 bytes
    And the truncator returns content of length 393216 with didTruncate=true
    And StagedDiffStat returns the string " vendored/bundle.min.js | 12345 +++++++++++++++++++++++++++++++++++"
    When CommitService.Commit processes that group
    Then GenerateRequest.Diff.Content sent to the LLM starts with the literal token "DIFF-SYNOPSIS"
    And GenerateRequest.Diff.Content contains the line "file: vendored/bundle.min.js"
    And GenerateRequest.Diff.Content contains the line "changes: +12345 / -0 (stat)"
    And GenerateRequest.Diff.Content contains the line "note: full diff elided (1048576 bytes exceeded 393216-byte cap)"
    And GenerateRequest.Diff.Content byte length is under 4096
    And stderr contains the line "commit 1/1: DIFF-SYNOPSIS fallback (vendored/bundle.min.js)"
    And the commit is created successfully
    And the hook (when configured) still receives the full raw 1048576-byte diff via HookInput.Diff

  @REQ-007 @synopsis @multi-file
  Scenario: Multi-file group with oversized total diff stays on the truncator path
    Given the staged diff is 500000 bytes across 4 files
    And the truncator returns content of length 393216 with didTruncate=true
    And the group's Files length is 4
    When CommitService.Commit processes that group
    Then GenerateRequest.Diff.Content sent to the LLM does not start with "DIFF-SYNOPSIS"
    And stderr contains the line "commit 1/N: truncating group diff (393216 bytes)"
    And StagedDiffStat is not invoked for this group
```

## Feature: Opt-in heuristic planner fallback

```gherkin
Feature: Opt-in heuristic planner fallback
  As a developer on an unreliable flash model
  I want git-agent to still produce commits when the LLM planner gives up
  So that I am not blocked from committing my work entirely

  Background:
    Given the project config at .git-agent/config.yml sets "plan_fallback: heuristic"
    And the project config declares scopes "cli", "app", "infra" with descriptions naming "cmd/", "application/", "infrastructure/"
    And the working tree has 6 changed files: 2 under cmd/, 2 under application/, 2 under infrastructure/

  @REQ-008 @fallback
  Scenario: Planner budget exhaustion triggers directory bucketing
    Given the LLM planner has returned commit.ErrPlannerBudgetExhausted
    When CommitService observes the exhausted error
    Then s.heuristicPlanner.Plan is invoked once with the same PlanRequest
    And directoryBucketer returns 3 commit groups: one per top-level directory
    And group 0 Files is ["cmd/<file>", "cmd/<file>"] mapped to scope "cli"
    And group 1 Files is ["application/<file>", "application/<file>"] mapped to scope "app"
    And group 2 Files is ["infrastructure/<file>", "infrastructure/<file>"] mapped to scope "infra"
    And stderr contains the line "planner exhausted budget — falling back to directoryBucketer"
    And the per-group Generate loop runs as normal
    And the process exits with exit code 0 on success

  @REQ-008 @fallback @default-off
  Scenario: With plan_fallback=none the existing hard-error path is preserved
    Given the project config at .git-agent/config.yml sets "plan_fallback: none"
    And the LLM planner has returned commit.ErrPlannerBudgetExhausted
    When CommitService observes the exhausted error
    Then s.heuristicPlanner.Plan is not invoked
    And the cmd layer renders the actionable budget-exhausted error to stderr
    And the process exits with exit code 1
```

## Feature: Backward compatibility and observability discipline

```gherkin
Feature: Backward compatibility and observability discipline
  As the project maintainer
  I want the redesign to leave the existing test suite green and to never leak secrets to stderr
  So that the change is safe to ship and observable in the wild

  @REQ-009 @regression
  Scenario: Existing small-diff happy path is unchanged
    Given a git repository at /tmp/tiny-diff-fixture with 1 changed file containing a 200-byte diff
    And a fake LLM endpoint that returns a plan and a single message in under 1 second each
    When I run "git-agent commit" without --verbose
    Then the process exits with exit code 0
    And stderr contains zero heartbeat lines matching "^still waiting on LLM..."
    And stderr contains at most 2 always-on phase lines
    And stdout contains the resulting commit hash and explanation

  @REQ-009 @regression
  Scenario: go test ./... passes after the redesign
    Given the working tree contains the redesign changes
    When I run "go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/..."
    Then every test package reports PASS
    And no test reports a goroutine leak

  @REQ-011 @security
  Scenario: Heartbeat and phase lines emit only metadata
    Given a fake LLM endpoint that takes 30 seconds to respond
    And the resolved API key is the string "sk-secret-key-redact-me-001"
    And the resolved base_url is "https://proxy.example.com/v1"
    When I run "git-agent commit" against the fixture
    Then stderr contains zero occurrences of the substring "sk-secret-key-redact-me-001"
    And stderr contains zero occurrences of the substring "proxy.example.com"
    And stderr contains zero occurrences of the prompt body or any diff content
    And stderr contains zero occurrences of any generated commit message before the final commit hash line
```

## Testing strategy

| Scenario | Test layer | Notes |
|---|---|---|
| REQ-001 per-attempt timeout | unit (`infrastructure/openai`) + e2e | Unit: `httptest.NewServer` with handler that hijacks and never writes. e2e: same with real binary. |
| REQ-002 SIGINT cancellation | e2e only | Real binary as subprocess; harness sends `cmd.Process.Signal(os.Interrupt)`. Requires signal handler in `main.go`. |
| REQ-003 always-on output | unit (`application` with fake writer) + 1 thin e2e | Service-level test captures stderr via `bytes.Buffer`; e2e confirms real subprocess. |
| REQ-004 heartbeat | unit (`infrastructure/openai`) with `goleak` | Inject a fake clock or `time.Ticker` channel; assert tick count, label, goroutine exit. |
| REQ-005 token-doubling ceiling | unit (`infrastructure/openai`) | `httptest` server returns canned `finish_reason=length` responses; assert no third request. |
| REQ-006 actionable error | unit (`cmd`) | Render `*PlannerBudgetExhaustedError` through the error-path arm; assert stderr substrings. |
| REQ-007 DIFF-SYNOPSIS fallback | unit (`application`) + e2e | Service test with fake git client returning the 1 MB diff and the stat string; e2e with a real 1 MB file. |
| REQ-008 heuristic fallback | unit (`application`) | Inject a fake planner returning `ErrPlannerBudgetExhausted`; assert `directoryBucketer.Plan` is called. |
| REQ-009 regression | existing `go test ./...` | No new test code; existing suite must stay green. |
| REQ-010 verbose superset | unit (`application`) | Capture stderr twice (verbose on/off) against the same fake LLM; diff the streams. |
| REQ-011 no secret leakage | unit (`infrastructure/openai`) + integration | Inject a known-secret API key + base URL; grep stderr capture for either substring. |

## Test-only HTTP helpers

A shared `infrastructure/openai/internal/fakeserver_test.go` provides:

- `newStallServer(t)` — `httptest.Server` that hijacks the connection and never writes; used by REQ-001 and REQ-002.
- `newSlowServer(t, delay time.Duration, body string)` — sleeps `delay` then writes `body`; used by REQ-004 and REQ-011.
- `newLengthServer(t, finishReasons ...string)` — returns one canned response per call with the given `finish_reason`; used by REQ-005.

E2E tests reuse the same helpers via a `e2e/fakellm_test.go` re-export.
