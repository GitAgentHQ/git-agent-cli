# Task 005: Payload parse & redaction (impl)

**type**: impl
**depends-on**: ["004"]

## Files
- modify: `cmd/capture_payload.go` — expand `claudeHookPayload` to the full
  PostToolUse schema; `mergeHookPayload` builds a `domain/graph.EventRecord` from
  the redacted payload, retaining the raw post-redaction stdin bytes as
  `PayloadRaw`.
- create: the redaction component — `infrastructure/redact/redactor.go` (Clean
  Architecture: redaction is an infrastructure concern — it spawns no business
  logic, has no domain port consumers beyond the cmd composition layer, and the
  compiled regex set + path denylist are implementation detail; keeping it in
  `infrastructure/` mirrors the existing `infrastructure/graph` hashing/ID
  placement. A `pkg/redact` location is acceptable if it must be importable without
  the `domain` dependency; pick one and stay consistent with task-004's import).

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

Make task-004 GREEN. Signatures only — NO bodies.

### A. Full payload struct + EventRecord build (cmd/capture_payload.go)

Expand `claudeHookPayload` to the full PostToolUse schema (architecture.md
"Claude PostToolUse Payload Mapping"); `ToolInput`/`ToolResponse` as
`json.RawMessage` so redaction can splice without losing bytes (best-practices.md
§1.3 — retain exact stored bytes):

```go
type claudeHookPayload struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`
}

// buildEventRecord parses the piped payload, redacts it, and returns the
// EventRecord whose PayloadRaw is the exact post-redaction bytes. ok is false
// when stdin is empty/interactive or the JSON fails to parse — caller must not
// append and must not error (FR13).
func buildEventRecord(source, tool, instanceID string, stdin []byte, r redact.Redactor) (graph.EventRecord, bool)
```

`mergeHookPayload` keeps its existing empty-stdin / unknown-field / parse-failure
no-op contract (interactive and malformed scenarios). The retained `PayloadRaw`
must be the bytes that get hashed — never a re-marshal of the struct.

### B. Redaction component (infrastructure/redact/redactor.go)

Per best-practices.md §1.1 (Layer A path denylist + Layer B compiled-once token
regex set) and §1.4 (Redaction Digest vs File Blob Ref):

```go
type Result struct {
	Bytes       []byte // post-redaction payload bytes
	OrigSize    int64  // size before truncation
	Truncated   bool
}

type Redactor interface {
	// Redact applies the path denylist, the compiled token-regex set, and the
	// size cap to the payload, returning the bytes to store plus size/truncation
	// metadata. Pure; no DB/git/network.
	Redact(payload []byte) Result
}

// NewRedactor builds a Redactor with the denylist, the once-compiled regex set,
// and the size cap from best-practices.md §1.1.
func NewRedactor() Redactor

// redactionDigest formats a Redaction Digest (sha256 + length) substituted for
// denylisted-path content.
// typedPlaceholder formats "[REDACTED:<rule-id>]" for a token match.
```

Compile the regex set once (package-level), pre-filter with substring gates,
cap scan size — best-practices.md §2 pitfall 7 (no shell-out, hot-path safe).

## Steps
1. Re-read architecture.md "Claude PostToolUse Payload Mapping" and best-practices.md
   §1.1, §1.3, §1.4.
2. Add the redaction component file at the location task-004's test imports; define
   `Redactor`, `Result`, `NewRedactor`, and the digest/placeholder helpers. The
   executor fills these in during Green to satisfy task-004's redaction unit.
3. Expand `claudeHookPayload`; add `buildEventRecord`; retain post-redaction raw
   bytes as `PayloadRaw`; preserve the no-op contract for empty/interactive/malformed.
4. Run gofmt; confirm imports compile against `domain/graph.EventRecord` (task-001).

## Verification
- `go test ./cmd/... ./infrastructure/... ./pkg/...` — task-004 GREEN.
- `go build ./...` — succeeds.
- `gofmt -l cmd/capture_payload.go infrastructure/redact/` — prints nothing.
