package graph

// SessionNode represents a group of sequential agent actions.
type SessionNode struct {
	ID         string
	Source     string // "claude-code", "cursor", "windsurf", "human"
	InstanceID string // distinguishes concurrent agents of same source (e.g., PID)
	StartedAt  int64
	EndedAt    int64 // 0 if still active
}

// ActionNode represents a single agent tool call or human edit.
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

// CaptureRequest is the input for recording an agent action.
type CaptureRequest struct {
	Source     string
	Tool       string
	InstanceID string
	Message    string
	EndSession bool
}

// CaptureResult is the output of a capture operation.
type CaptureResult struct {
	ActionID     string   `json:"action_id"`
	SessionID    string   `json:"session_id"`
	FilesChanged []string `json:"files_changed"`
	CaptureMs    int64    `json:"capture_ms"`
	Skipped      bool     `json:"skipped,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

// CaptureBaseline tracks file content hashes for delta-based capture.
type CaptureBaseline struct {
	FilePath    string
	ContentHash string
	CapturedAt  int64
}
