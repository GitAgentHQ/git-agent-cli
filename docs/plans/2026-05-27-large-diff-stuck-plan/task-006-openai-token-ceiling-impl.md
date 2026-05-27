# Task 006 — OpenAI token-doubling ceiling (impl)

| Field | Value |
|---|---|
| **subject** | Cap `MaxCompletionTokens` doubling at per-endpoint ceilings; return typed error |
| **type** | impl |
| **depends-on** | ["006-openai-token-ceiling-test"] |
| **REQ refs** | REQ-005 |
| **layer** | infrastructure/openai |

## Files to modify

- `infrastructure/openai/client.go` — extend `callLLM` signature with `maxTokensCeiling`; change `FinishReasonLength` branch; update four call sites

## Interface contracts

```go
const (
    planMaxTokensCeiling     = 16384
    generateMaxTokensCeiling = 16384
    scopesMaxTokensCeiling   = 16384
    detectMaxTokensCeiling   = 4096
)

func (c *Client) callLLM(
    ctx context.Context,
    system, user string,
    maxTokens, maxTokensCeiling int,
) (string, error)
```

## Implementation steps

1. Declare the four package-scope `*MaxTokensCeiling` constants.
2. Extend `callLLM` signature with `maxTokensCeiling int`.
3. Replace the unconditional `req.MaxCompletionTokens *= 2` block (`client.go:233-238`) with a ceiling-aware version: if `next := req.MaxCompletionTokens * 2; next > maxTokensCeiling`, return `&commit.PlannerBudgetExhaustedError{Model: c.model, Ceiling: maxTokensCeiling}` immediately; else set and continue.
4. Update the four call sites (`Generate` ~363, `Plan` ~420, `DetectTechnologies` ~461, `GenerateScopes` ~491) to pass the matching ceiling.
5. Import `github.com/gitagenthq/git-agent/domain/commit` for the error type (outer→inner; allowed by Clean Architecture).

## Verification

```bash
go test -count=1 ./infrastructure/openai/... ./domain/commit/...
```

Task-006-test passes; existing openai tests stay green.
