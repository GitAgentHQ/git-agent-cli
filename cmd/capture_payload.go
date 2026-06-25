package cmd

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/infrastructure/redact"
)

// claudeHookPayload is the Claude Code PostToolUse stdin payload that capture
// observes. Unknown fields are ignored so the format can evolve without breaking
// the hook. tool_input/tool_response are kept raw so redaction can splice them
// without re-serialization drift.
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

// mergeHookPayload overlays a Claude Code hook payload onto the capture flags.
// Explicit flags always win; the payload only fills what the caller left empty.
// Any parse failure is silently ignored — a PostToolUse hook must never error
// out and block the agent.
func mergeHookPayload(tool, instanceID string, stdin []byte) (string, string) {
	if len(stdin) == 0 {
		return tool, instanceID
	}
	var p claudeHookPayload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return tool, instanceID
	}
	if tool == "" {
		tool = p.ToolName
	}
	if instanceID == "" {
		instanceID = p.SessionID
	}
	return tool, instanceID
}

// buildEventRecord parses the piped PostToolUse payload, redacts it, and returns
// the EventRecord whose PayloadRaw is the exact post-redaction bytes plus the
// redacted Bash tool_response (used by capture to derive the Outcome exit code,
// never stored separately). ok is false when stdin is empty/interactive or the
// JSON fails to parse — the caller must not append and must not error (a hook
// never blocks the agent). Explicit tool/instanceID arguments override the
// payload's tool_name/session_id.
func buildEventRecord(source, tool, instanceID string, stdin []byte, r redact.Redactor) (graph.EventRecord, []byte, bool) {
	if len(stdin) == 0 {
		return graph.EventRecord{}, nil, false
	}
	var p claudeHookPayload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return graph.EventRecord{}, nil, false
	}

	res := r.Redact(stdin)

	if tool == "" {
		tool = p.ToolName
	}
	if instanceID == "" {
		instanceID = p.SessionID
	}

	// Re-parse the redacted payload so the command and tool_response carried into
	// the Outcome path match the redacted bytes that are actually stored.
	var redacted claudeHookPayload
	_ = json.Unmarshal(res.Bytes, &redacted)

	return graph.EventRecord{
		RecordedAt:     time.Now().Unix(),
		Source:         graph.EventSource(source),
		InstanceID:     instanceID,
		Kind:           graph.EventKindTool,
		HookEventName:  p.HookEventName,
		ToolName:       tool,
		Cwd:            p.Cwd,
		TranscriptPath: p.TranscriptPath,
		PermissionMode: p.PermissionMode,
		PayloadRaw:     res.Bytes,
		PayloadSize:    res.OrigSize,
		Truncated:      res.Truncated,
		Command:        bashCommand(redacted.ToolInput),
	}, redacted.ToolResponse, true
}

// bashCommand extracts the `command` field from a Bash tool_input. Returns empty
// for non-Bash payloads or when the field is absent.
func bashCommand(toolInput json.RawMessage) string {
	if len(toolInput) == 0 {
		return ""
	}
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(toolInput, &in); err != nil {
		return ""
	}
	return in.Command
}

// readPipedStdin returns stdin contents when it is piped/redirected (a hook
// payload), or nil when it is an interactive terminal. It never blocks waiting
// for a human to type.
func readPipedStdin() []byte {
	info, err := os.Stdin.Stat()
	if err != nil || (info.Mode()&os.ModeCharDevice) != 0 {
		return nil // interactive terminal — no payload
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return nil
	}
	return data
}
