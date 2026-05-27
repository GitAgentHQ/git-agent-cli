# Task 006 — OpenAI token-doubling ceiling (test)

| Field | Value |
|---|---|
| **subject** | Add unit test asserting per-endpoint ceiling halts retries with typed error |
| **type** | test |
| **depends-on** | ["002-domain-errors-impl"] |
| **REQ refs** | REQ-005 |
| **layer** | infrastructure/openai |

## Files to modify

- `infrastructure/openai/client_test.go` — add `TestClient_TokenCeiling`
- `infrastructure/openai/internal/fakeserver_test.go` (or inline) — add `newLengthServer` returning canned `finish_reason=length` responses

## BDD Scenario

```gherkin
Scenario: Planner doubling halts at the ceiling after one attempted double
  Given a fake LLM endpoint that returns finish_reason=length on attempt 1 with max_completion_tokens=8192
  And the same fake returns finish_reason=length on attempt 2 with max_completion_tokens=16384
  When git-agent issues the planner call
  Then a third HTTP request to the planner endpoint is never sent
  And callLLM returns an error of type *commit.PlannerBudgetExhaustedError
  And the error carries Model "deepseek-v4-flash" and Ceiling 16384
```

## Acceptance criteria

- Test starts a `httptest.NewServer` that returns one canned `finish_reason=length` chat-completion response per request.
- Test counts the number of incoming HTTP requests.
- Test invokes `client.Plan(ctx, req)` with seed `MaxCompletionTokens=8192` and ceiling `16384`.
- Asserts exactly 2 HTTP requests reach the server (not 3).
- Asserts the returned error satisfies `errors.As(err, &target)` where `target` is `*commit.PlannerBudgetExhaustedError`.
- Asserts `target.Model == "deepseek-v4-flash"` and `target.Ceiling == 16384`.
- Asserts `errors.Is(err, commit.ErrPlannerBudgetExhausted)` returns true.

## Verification

```bash
go test -count=1 -run 'TestClient_TokenCeiling' ./infrastructure/openai/...
```

Fails (RED) until task-006-impl lands.
