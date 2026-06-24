package cmd

import (
	"encoding/json"
	"io"
	"os"
)

// claudeHookPayload is the subset of the Claude Code PostToolUse stdin payload
// that capture cares about. Unknown fields are ignored so the format can evolve
// without breaking the hook.
type claudeHookPayload struct {
	ToolName  string `json:"tool_name"`
	SessionID string `json:"session_id"`
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
