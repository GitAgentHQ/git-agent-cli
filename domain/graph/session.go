package graph

// SessionNode represents a group of sequential agent actions. It is a Projection
// DTO derived from the Event Log, not a source-of-truth record.
type SessionNode struct {
	ID         string
	Source     string // "claude-code", "cursor", "windsurf", "human"
	InstanceID string // distinguishes concurrent agents of same source (e.g., PID)
	StartedAt  int64
	EndedAt    int64 // 0 if still active
}

// SessionIDGenerator produces unique session identifiers. Generating IDs is an
// infrastructure concern (UUID, ULID, etc.), so the application layer depends
// on this port rather than on a concrete ID library.
type SessionIDGenerator interface {
	NewSessionID() string
}

// ActionNode represents a single agent tool call or human edit. It is a
// Projection DTO derived from the Event Log, not a source-of-truth record.
type ActionNode struct {
	ID           string // "{session_id}:{sequence}"
	SessionID    string
	Sequence     int
	Tool         string // "Edit", "Write", "Bash", "manual-save"
	Diff         string // unified diff (truncated at 100KB)
	FilesChanged []string
	Timestamp    int64
	Message      string
}

// CaptureRequest is the input for recording an agent action. Event carries the
// observed, already-redacted hook payload to append; it is nil when the caller
// has no payload (interactive/malformed) or is ending a session.
type CaptureRequest struct {
	Source     string
	Tool       string
	InstanceID string
	Message    string
	EndSession bool
	Event      *EventRecord
	// ToolResponse is the redacted Bash tool_response bytes, used only to derive
	// the Outcome Event exit code. It is a transient classification input, not a
	// stored field — the hashed unit remains Event.PayloadRaw.
	ToolResponse []byte
}

// CaptureResult is the output of a capture operation.
type CaptureResult struct {
	EventID   string `json:"event_id,omitempty"`
	Seq       int64  `json:"seq,omitempty"`
	Source    string `json:"source,omitempty"`
	CaptureMs int64  `json:"capture_ms"`
	Skipped   bool   `json:"skipped,omitempty"`
	Reason    string `json:"reason,omitempty"`
}
