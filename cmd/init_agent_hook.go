package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	agentHookMatcher = "Edit|Write|Bash"
	agentHookCommand = "git-agent capture --source claude-code"
)

// upsertAgentHook merges a Claude Code PostToolUse hook that runs git-agent
// capture into an existing settings.json blob (or creates one from nil/empty),
// preserving every other key. It is idempotent: if a PostToolUse hook already
// invokes git-agent capture, the input is returned with formatting normalized
// and no duplicate is added.
func upsertAgentHook(existing []byte) ([]byte, error) {
	root := map[string]any{}
	if len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("parse settings.json: %w", err)
		}
	}

	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	post, _ := hooks["PostToolUse"].([]any)

	for _, e := range post {
		entry, _ := e.(map[string]any)
		inner, _ := entry["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, "git-agent capture") {
				// Already installed — return normalized, no duplicate.
				return marshalSettings(root)
			}
		}
	}

	post = append(post, map[string]any{
		"matcher": agentHookMatcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": agentHookCommand},
		},
	})
	hooks["PostToolUse"] = post
	root["hooks"] = hooks
	return marshalSettings(root)
}

func marshalSettings(root map[string]any) ([]byte, error) {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// installAgentHook writes (or updates) the PostToolUse capture hook in
// <root>/.claude/settings.json, preserving any existing configuration.
func installAgentHook(root string) (string, error) {
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	existing, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", settingsPath, err)
	}

	merged, err := upsertAgentHook(existing)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return "", fmt.Errorf("create .claude dir: %w", err)
	}
	if err := os.WriteFile(settingsPath, merged, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", settingsPath, err)
	}
	return settingsPath, nil
}
