# Task 004: Payload parse & redaction (test)

**type**: test
**depends-on**: ["001"]

## Files
- create: `cmd/capture_payload_test.go` — expand existing table tests with full
  PostToolUse parsing + `EventRecord` build cases; malformed-JSON and
  interactive (no piped stdin) no-op cases.
- create: the redaction component test (place beside the impl chosen in
  task-005, e.g. `infrastructure/redact/redactor_test.go` or
  `pkg/redact/redactor_test.go`) — path denylist, token regex, size cap.

## BDD Scenario(s)
```gherkin
Scenario: A secret token in the payload is redacted before storage
  Given a Bash payload whose command contains an AWS access key
  When capture is invoked
  Then payload_raw contains a Typed Placeholder "[REDACTED:aws-access-token]"
  And the raw key value never appears in graph.db

Scenario: A sensitive file path is stored as a Redaction Digest
  Given an Edit payload whose file_path is ".env"
  When capture is invoked
  Then the Event stores a Redaction Digest (sha256 + length) instead of file contents
  And no .env line content appears in graph.db

Scenario: An oversized payload is bounded
  Given a tool_response exceeding the payload size cap
  When capture is invoked
  Then the stored Event is truncated with the truncation flag set and payload_size recorded
  And this_hash is computed over exactly the stored bytes

Scenario: A malformed payload never errors the hook
  Given stdin contains the bytes `{not json` that fail JSON parse
  When capture is invoked
  Then no Event is appended
  And capture exits 0 with a stderr warning

Scenario: Interactive invocation with no piped payload is a no-op for payload merge
  Given capture is run from an interactive terminal with no stdin payload
  When capture is invoked
  Then no payload fields are merged and behavior falls back to explicit flags
```

## What to implement

Two RED test groups. No production code; tests reference symbols that do not yet
exist (or exercise the not-yet-expanded parser) and must fail to compile/run for
the right reason.

### A. Payload parse → EventRecord (cmd)

Tests for the expanded `claudeHookPayload` and a payload→`EventRecord` build path
per architecture.md "Claude PostToolUse Payload Mapping" (the full field table:
`session_id`→`InstanceID`, `transcript_path`→`TranscriptPath`, `cwd`→`Cwd`,
`hook_event_name`→`HookEventName`, `permission_mode`→`PermissionMode`,
`tool_name`→`ToolName`, `tool_input`+`tool_response` redacted →`PayloadRaw`).

- Given a full PostToolUse JSON on stdin, assert the built `domain/graph.EventRecord`
  carries every mapped scalar field, `Source == "claude-code"`, `Kind == "tool"`,
  and `PayloadRaw` equals the post-redaction bytes (not a re-serialization of the
  parsed struct) — see best-practices.md §1.3 (retain exact stored bytes).
- Malformed JSON (`{not json`): no `EventRecord` produced, no error returned to the
  caller — the existing "unknown fields ignored / never error" contract
  (architecture.md "Claude PostToolUse Payload Mapping").
- Interactive (no piped stdin → `readPipedStdin()` returns nil): no payload fields
  merged; explicit flags pass through unchanged (preserve current
  `mergeHookPayload` empty-stdin behavior).

Reuse the existing `TestMergeHookPayload` table style on the branch
(`cmd/capture_payload_test.go`).

### B. Redaction unit

Tests for the `Redact` contract introduced in task-005 (best-practices.md §1.1,
§1.4):

- **Path denylist → Redaction Digest.** A sensitive path field (`.env`, `*.pem`,
  `*.key`, `id_rsa`, `credentials`, `.npmrc`, `.netrc`, `*.tfvars`, …) yields a
  Redaction Digest (`sha256` + length) in place of contents; assert the original
  secret content bytes are absent from the output.
- **Token regex → Typed Placeholder.** An AWS key `(AKIA|ASIA)[A-Z0-9]{16}` is
  replaced with `[REDACTED:aws-access-token]`; the raw key value never survives.
  Cover at least one additional format (GitHub `ghp_…`, Slack `xox…`, or PEM block)
  to lock the rule-id naming.
- **Oversized → truncation + flag.** Input over the size cap is truncated; the
  result reports a truncation flag and the original `payload_size`; downstream
  `this_hash` must cover exactly the stored (truncated) bytes (assert the stored
  byte length equals what the flag/size report).

Isolate from DB/git/network: these are pure-function tests over byte
inputs/outputs; no SQLite, no subprocess, no `os.Stdin` beyond the cmd-layer
`readPipedStdin` seam.

## Steps
1. Read architecture.md "Claude PostToolUse Payload Mapping" and best-practices.md
   §1.1, §1.3, §1.4 for the exact field set, placeholder format, and bytes-retention
   rule.
2. Read the existing `cmd/capture_payload_test.go` (branch) to match table style;
   extend it with full-payload, malformed, and interactive cases that assert on the
   built `EventRecord` (referencing the task-005 build function and the
   `domain/graph.EventRecord` type from task-001).
3. Write the redaction unit test referencing the `Redact` function/type from
   task-005 with the path-denylist, token-regex, and size-cap cases above.
4. Confirm the redaction test package import path matches the location task-005
   will choose; leave a `// task-005:` note if the path is provisional.

## Verification
- `go test ./cmd/... ./infrastructure/... ./pkg/...` — RED: tests fail to compile
  or fail assertions because the expanded parser, the `EventRecord` build path, and
  the `Redact` component do not exist yet. Confirm each failure is for the intended
  missing symbol, not an unrelated error.
