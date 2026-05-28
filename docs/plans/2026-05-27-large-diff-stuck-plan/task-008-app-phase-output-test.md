# Task 008 — Application phase output (test)

| Field | Value |
|---|---|
| **subject** | Add unit tests for always-on phase lines + verbose superset property |
| **type** | test |
| **depends-on** | [] |
| **REQ refs** | REQ-003, REQ-010 |
| **layer** | application |

## Files to modify

- `application/commit_service_test.go` — add `TestCommitService_AlwaysOnPhaseLines`, `TestCommitService_VerboseIsSuperset`
- `application/verbose_test.go` — extend / adapt existing test for the verbose-superset assertion

## BDD Scenarios

```gherkin
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
```

```gherkin
Scenario: --verbose output is a strict superset of always-on output
  Given the same fixture and fake endpoint as the default-mode scenario above
  When I run "git-agent commit --verbose" and capture stderr as stream V
  And I run "git-agent commit" without --verbose and capture stderr as stream A
  Then every line in stream A appears exactly once in stream V
  And every line in stream A appears at most once in stream A
  And stream V contains additional lines absent from stream A, including "staged files:" and "unstaged files:"
  And no line is duplicated within either stream
```

## Acceptance criteria

- Test builds a `CommitRequest` with `Verbose: false` and a `bytes.Buffer` as `OutWriter`; invokes `svc.Commit(ctx, req)` against fakes that return 2 planned groups.
- Asserts captured stderr contains `"planning"`, `"planned 2 commit(s)"`, `"commit 1/2: drafting message (attempt 1/3)"`, `"commit 2/2: drafting message (attempt 1/3)"`.
- Asserts captured stderr does NOT contain any substring from the diff content.
- Second test: capture two streams (verbose on/off) against the same fakes; assert each always-on line appears exactly once in both streams; verbose stream contains additional lines starting with `"staged files:"` or `"unstaged files:"`.

## Verification

```bash
go test -count=1 -run 'TestCommitService_AlwaysOnPhaseLines|TestCommitService_VerboseIsSuperset' ./application/...
```

Fails (RED) until task-008-impl lands.
