# Task 014 — Security (test-only)

| Field | Value |
|---|---|
| **subject** | Confirm heartbeat and phase lines emit only metadata, no secret leakage |
| **type** | test |
| **depends-on** | ["005-openai-heartbeat-impl", "008-app-phase-output-impl"] |
| **REQ refs** | REQ-011 |
| **layer** | infrastructure/openai + application |

## Files to modify

- `infrastructure/openai/client_test.go` — add `TestClient_HeartbeatNoSecretLeakage`
- `application/commit_service_test.go` — add `TestCommitService_PhaseLinesNoSecretLeakage`

## BDD Scenario

```gherkin
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

## Acceptance criteria

- Heartbeat test: construct `openai.Client` with API key `"sk-secret-key-redact-me-001"`, base URL `"https://proxy.example.com/v1"`, model `"gpt-x"`; force a slow response so ≥2 ticks fire. Capture buffer. Assert buffer contains zero occurrences of the key or the base URL host.
- Phase-line test: feed a `CommitRequest` whose `Intent` and diff content contain a sentinel string `"SECRET-DIFF-CONTENT-NEVER-LOG"`. Capture both `OutWriter` and `LogWriter` streams. Assert the always-on stream contains zero occurrences of the sentinel.

## Verification

```bash
go test -count=1 -run 'TestClient_HeartbeatNoSecretLeakage|TestCommitService_PhaseLinesNoSecretLeakage' ./infrastructure/openai/... ./application/...
```

No implementation task. This is an invariant test.
