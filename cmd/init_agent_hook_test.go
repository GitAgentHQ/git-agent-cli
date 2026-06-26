package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// countCaptureHooks walks a settings.json blob and counts PostToolUse hook
// entries whose command invokes git-agent capture.
func countCaptureHooks(t *testing.T, blob []byte) int {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(blob, &root); err != nil {
		t.Fatalf("invalid settings json: %v\n%s", err, blob)
	}
	hooks, _ := root["hooks"].(map[string]any)
	post, _ := hooks["PostToolUse"].([]any)
	n := 0
	for _, e := range post {
		entry, _ := e.(map[string]any)
		inner, _ := entry["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, "git-agent capture") {
				n++
			}
		}
	}
	return n
}

func TestUpsertAgentHook_CreatesFromEmpty(t *testing.T) {
	out, err := upsertAgentHook(nil)
	if err != nil {
		t.Fatalf("upsertAgentHook: %v", err)
	}
	if got := countCaptureHooks(t, out); got != 1 {
		t.Fatalf("capture hooks = %d, want 1", got)
	}
	// Matcher must cover the three mutating tools.
	if !strings.Contains(string(out), "Edit") || !strings.Contains(string(out), "Write") || !strings.Contains(string(out), "Bash") {
		t.Errorf("matcher missing Edit/Write/Bash:\n%s", out)
	}
	if !strings.Contains(string(out), "--source claude-code") {
		t.Errorf("command missing source:\n%s", out)
	}
}

func TestUpsertAgentHook_PreservesExisting(t *testing.T) {
	existing := []byte(`{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo hi"}]}]}
}`)
	out, err := upsertAgentHook(existing)
	if err != nil {
		t.Fatalf("upsertAgentHook: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := root["permissions"]; !ok {
		t.Error("permissions key was dropped")
	}
	hooks := root["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("existing PreToolUse hook was dropped")
	}
	if got := countCaptureHooks(t, out); got != 1 {
		t.Errorf("capture hooks = %d, want 1", got)
	}
}

func TestUpsertAgentHook_Idempotent(t *testing.T) {
	once, err := upsertAgentHook(nil)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := upsertAgentHook(once)
	if err != nil {
		t.Fatal(err)
	}
	if got := countCaptureHooks(t, twice); got != 1 {
		t.Fatalf("after second upsert capture hooks = %d, want 1", got)
	}
}
