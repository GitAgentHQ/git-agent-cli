# Task 009: Outcome Events (impl)

**type**: impl
**depends-on**: ["008", "007"]

## Files
- create: `application/outcome_classifier.go` — deterministic test/build command
  classifier (`ClassifyCommand`) and Bash `tool_response` exit-code extraction.
- modify: `application/capture_service.go` — when the parsed payload is a Bash tool
  call, build an Outcome `EventRecord` (`Kind == "outcome"`) and append it on the
  same chain as any other Event.
- modify: `cmd/capture_payload.go` — surface the parsed `tool_name`,
  `tool_input.command`, and `tool_response` fields needed to build the Outcome
  Event, carried into the `EventRecord` (after redaction from task-005).

This makes task-008 GREEN.

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

Reference: `architecture.md` "Outcome Event capture" and "Claude PostToolUse Payload
Mapping"; `best-practices.md` pitfalls 13–14 and §2.1 (no shelling out on the hot
path). NO function bodies — signatures and prose only.

### Layer placement (Clean Architecture justification)

The classifier is a **pure decision over a command string** with zero I/O — no git,
no DB, no network. Under `cmd → application → domain ← infrastructure`, this is
orchestration-adjacent business logic with no external dependency, so it belongs in
`application/` (peer of `CaptureService`), not `infrastructure/` (which is reserved
for adapters) and not `domain/` (which forbids it being an interface — there is one
deterministic algorithm, not a swappable port; per the global guideline "2 variants
= if/switch, not an abstraction layer"). The capture path already lives in
`application/CaptureService`, so the Outcome branch sits there too.

### Classifier (`application/outcome_classifier.go`)

```go
// OutcomeClassification is the deterministic verdict for a Bash command.
type OutcomeClassification struct {
    IsTest   bool
    IsBuild  bool
    TestName string
}

// ClassifyCommand deterministically classifies a Bash command as test and/or
// build, extracting a test name from syntax that exposes one (e.g. `go test -run
// <name>`). Recognized test forms include go test, make test, pnpm test, pytest,
// cargo test; build forms include go build, make build. Unrecognized commands
// classify as neither.
func ClassifyCommand(command string) OutcomeClassification

// ExtractReportedExitCode returns the exit code stated in a Bash tool_response
// and whether one was present. Used to distinguish "reported" from "inferred".
func ExtractReportedExitCode(toolResponse []byte) (code int, ok bool)

// InferExitCode derives a best-effort exit code from output failure markers
// (e.g. "FAIL") when no exit code is reported. The result is always flagged
// "inferred" by the caller and down-weighted in diagnose (best-practices.md
// pitfall 13). Returns the inferred code and whether a failure marker was seen.
func InferExitCode(toolResponse []byte) (code int, sawFailure bool)
```

Exit-code source resolution in `CaptureService`:

- `ExtractReportedExitCode` succeeds → `ExitCode` set, `ExitCodeSource = "reported"`.
- Otherwise `InferExitCode` → if a failure marker is seen, `ExitCode` non-zero,
  `ExitCodeSource = "inferred"`; if no marker, treat as success (0, inferred) or
  leave per the deterministic rule the tests pin.

### Capture-service Outcome branch (`application/capture_service.go`)

When `tool_name == "Bash"`, build an `EventRecord` with `Kind = "outcome"`,
`Command` = the parsed command, `IsTest`/`IsBuild`/`TestName` from
`ClassifyCommand`, and `ExitCode`/`ExitCodeSource` from the resolution above, then
append it via the same `repo.AppendEvent` chain step used for tool Events (from
task-007). No `DiffNameOnly`/`HashObject` on this path.

### Honest limits (encode, do not paper over)

- **Compound commands** (`go build && go test`) expose only an **aggregate** exit
  code from `tool_response`; record that single aggregate, do not invent per-segment
  results. Classification may set both `IsBuild` and `IsTest` when both verbs are
  present.
- **Parse failure DROPS the Outcome Event** — if the command or `tool_response`
  cannot be parsed, do not append an Outcome Event and never fabricate one
  (`best-practices.md` pitfall 14). The hook still exits 0 (FR13). A tool Event may
  still be appended by the normal path; only the Outcome classification is dropped.

## Steps

1. Add `application/outcome_classifier.go` with `ClassifyCommand`,
   `ExtractReportedExitCode`, `InferExitCode` signatures (no bodies in the plan;
   implement in execution).
2. Extend `cmd/capture_payload.go` so `tool_input.command` and `tool_response` reach
   the `EventRecord` build (post-redaction), preserving the "unknown fields ignored,
   never error" rule.
3. Add the `tool_name == "Bash"` Outcome branch to `CaptureService.Capture`: classify,
   resolve exit code/source, build the Outcome `EventRecord`, append on the chain;
   drop on parse failure.
4. Run task-008 and confirm GREEN; run `go build ./...` and `gofmt -l`.

## Verification

- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...`
  — task-008 tests pass (GREEN).
- `go build ./...` — succeeds.
- `gofmt -l application/outcome_classifier.go application/capture_service.go cmd/capture_payload.go`
  — prints nothing (formatting clean).
