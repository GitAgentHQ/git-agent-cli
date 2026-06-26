# Task 008: Outcome Events (test)

**type**: test
**depends-on**: ["001"]

## Files
- create: `application/outcome_classifier_test.go` — RED tests for the deterministic
  test/build command classifier (table-driven, no DB/git/network).
- create: `application/outcome_capture_test.go` — RED tests for Outcome Event
  capture from a Bash payload via `CaptureService.Capture`, using test doubles for
  the repository and git client (no real DB, no shelling out).

These are RED tests only. Production code (classifier + capture wiring) lands in
task-009; until then these must fail to **compile or assert**, observed failing for
the right reason (the classifier function / outcome fields do not yet exist), per
the BDD Iron Law in `best-practices.md` §3.

## BDD Scenario(s)

```gherkin
  Scenario: A failing test command is recorded as an Outcome Event
    Given a Bash payload running "go test ./..." whose tool_response reports exit code 1
    When capture is invoked
    Then an Outcome Event is appended with kind "outcome", is_test true, exit_code 1
    And exit_code_source is "reported"
    And the Outcome Event is chained like any other Event

  Scenario: An inferred exit code is marked as inferred
    Given a Bash payload running "make test" whose tool_response has no explicit exit field
    And whose output contains "FAIL"
    When capture is invoked
    Then the Outcome Event exit_code is non-zero
    And exit_code_source is "inferred"

  Scenario: A non-test Bash command records an exit code without test classification
    Given a Bash payload running "ls" whose tool_response reports exit code 0
    When capture is invoked
    Then an Outcome Event is appended with is_test false and is_build false
```

## What to implement

Failing tests asserting the behavior task-009 will provide. Clean Architecture:
the classifier is a pure function in the `application` layer (no infra imports), so
the test lives in `application_test`. The capture-path test exercises
`application.CaptureService.Capture` with a Bash-shaped `CaptureRequest`/payload and
test doubles.

Reference: `architecture.md` "Outcome Event capture"; `best-practices.md` pitfalls
13 (inferred exit codes are not ground truth) and 14 (never fabricate Outcome
Events).

### Classifier tests (`outcome_classifier_test.go`)

Target a deterministic, pure classifier with a signature shaped like:

```go
// OutcomeClassification is the deterministic verdict for a Bash command.
type OutcomeClassification struct {
    IsTest   bool
    IsBuild  bool
    TestName string
}

// ClassifyCommand inspects a Bash command string and deterministically
// classifies it as a test and/or build invocation, extracting a test name
// where the command syntax exposes one (e.g. `go test -run`).
func ClassifyCommand(command string) OutcomeClassification
```

Table-driven cases (assert `IsTest`/`IsBuild`/`TestName`):

- `go test ./...` → test true, build false.
- `go test ./application/... -run TestCommitService_NoStagedChanges` → test true,
  `TestName` = `TestCommitService_NoStagedChanges` (extracted from `-run`).
- `make test` → test true.
- `pnpm test` → test true.
- `pytest tests/` → test true.
- `cargo test` → test true.
- `go build ./...` → build true, test false.
- `make build` → build true.
- `ls` → test false, build false.
- `git status` → test false, build false.

### Outcome-capture tests (`outcome_capture_test.go`)

Drive `CaptureService.Capture` with a Bash `CaptureRequest` carrying the
post-redaction payload bytes (`PayloadRaw`) for each scenario. Use a repository
test double that records the `graph.EventRecord` handed to `AppendEvent`, and a git
test double that fails the test if `DiffNameOnly`/`HashObject` are called on the hot
path (Outcome capture must not shell out — `architecture.md` §"Outcome Event
capture").

Assert the appended `EventRecord`:

- **Reported failing test** (`go test ./...`, `tool_response` exit code 1):
  `Kind == "outcome"`, `IsTest == true`, `ExitCode != nil && *ExitCode == 1`,
  `ExitCodeSource == "reported"`, and `ThisHash`/`PrevHash` are set (chained like
  any other Event).
- **Inferred failure** (`make test`, no explicit exit field, output contains
  `FAIL`): `ExitCode != nil && *ExitCode != 0`, `ExitCodeSource == "inferred"`.
- **Non-test command** (`ls`, reported exit code 0): `Kind == "outcome"`,
  `IsTest == false`, `IsBuild == false`.

## Steps

1. Add `application/outcome_classifier_test.go` with the table-driven cases above
   calling `application.ClassifyCommand` (not yet defined).
2. Add `application/outcome_capture_test.go` with a recording repository double and a
   strict git double; build a Bash `CaptureRequest` per scenario and assert the
   appended `EventRecord` fields.
3. Confirm the tests reference only `application` exports and the `domain/graph`
   types — no infrastructure imports beyond test setup helpers already used by
   `capture_service_test.go`.
4. Run the test base and confirm RED (compile/assert failure naming the missing
   classifier or outcome fields).

## Verification

- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...`
  — expected **RED**: `outcome_classifier_test.go` / `outcome_capture_test.go` fail
  to compile or assert because `ClassifyCommand` and the Outcome Event capture path
  do not exist yet. The failure must name the missing symbol/behavior, not an
  unrelated error.
