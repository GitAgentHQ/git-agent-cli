# Task 009 — Application DIFF-SYNOPSIS fallback (test)

| Field | Value |
|---|---|
| **subject** | Add unit tests for single-file synopsis fallback + multi-file truncator path |
| **type** | test |
| **depends-on** | ["003-git-stagediffstat-impl"] |
| **REQ refs** | REQ-007 |
| **layer** | application |

## Files to modify

- `application/commit_service_test.go` — add `TestCommitService_SynopsisFallbackOneFile`, `TestCommitService_TruncatorPathMultiFile`

## BDD Scenarios

```gherkin
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
```

```gherkin
Scenario: Multi-file group with oversized total diff stays on the truncator path
  Given the staged diff is 500000 bytes across 4 files
  And the truncator returns content of length 393216 with didTruncate=true
  And the group's Files length is 4
  When CommitService.Commit processes that group
  Then GenerateRequest.Diff.Content sent to the LLM does not start with "DIFF-SYNOPSIS"
  And stderr contains the line "commit 1/N: truncating group diff (393216 bytes)"
  And StagedDiffStat is not invoked for this group
```

## Acceptance criteria

- Test sets up a fake `CommitGitClient` whose `StagedDiff` returns 1 file with 1 MB of content; whose `StagedDiffStat` returns the canned stat string; whose `StageFiles` / `UnstageAll` / `Commit` succeed.
- Fake truncator returns content of exactly `maxBytes` length and `didTruncate=true`.
- Capture `GenerateRequest.Diff.Content` via a spy `CommitMessageGenerator`.
- Assert the captured content starts with `"DIFF-SYNOPSIS"`, contains the three labelled lines, total length under 4096.
- Assert `HookInput.Diff` passed to the hook is the original 1 MB unstripped content.
- Second test: 4-file group with 500 000 bytes total; assert `GenerateRequest.Diff.Content` does NOT start with `DIFF-SYNOPSIS`; assert `StagedDiffStat` is never called (count via spy).

## Verification

```bash
go test -count=1 -run 'TestCommitService_SynopsisFallback|TestCommitService_TruncatorPathMultiFile' ./application/...
```

Fails (RED) until task-009-impl lands.
