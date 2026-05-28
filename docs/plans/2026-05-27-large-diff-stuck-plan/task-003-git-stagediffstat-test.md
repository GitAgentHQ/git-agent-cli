# Task 003 — Git `StagedDiffStat` (test)

| Field | Value |
|---|---|
| **subject** | Add unit test for `(*git.Client).StagedDiffStat` |
| **type** | test |
| **depends-on** | [] |
| **REQ refs** | REQ-007 (prerequisite) |
| **layer** | infrastructure/git |

## Files to modify

- `infrastructure/git/client_test.go` — add `TestClient_StagedDiffStat`

## BDD Coverage

Foundation task. Provides the data source for the Scenario `Single-file 1 MB diff triggers the DIFF-SYNOPSIS fallback` (`bdd-specs.md:144`), which asserts the LLM payload includes the line `changes: +12345 / -0 (stat)` — that string is derived from `StagedDiffStat` output.

## Acceptance criteria

- Test creates a temp git repo with one staged 1 MB file.
- Test calls `client.StagedDiffStat(ctx)` and asserts the returned string contains the filename and a `+<num>` insertion count.
- Test asserts the function returns `(string, error)` and exits with no error.
- Test asserts the returned string ends with a `1 file changed, ` summary line.

## Verification

```bash
go test -count=1 -run 'TestClient_StagedDiffStat' ./infrastructure/git/...
```

Fails (RED) until task-003-impl lands.
