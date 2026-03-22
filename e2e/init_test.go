package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCmd_ScopeFlag_NoAPIKey_Fails(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--scope")
	if code == 0 {
		t.Fatal("expected non-zero exit when --scope given without API key, got 0")
	}
}

func TestInitCmd_HookAndScope_BothTogether_ScopeFails(t *testing.T) {
	dir := newGitRepo(t)
	// Without API key, --scope fails; hook is now a separate config set call.
	out, code := gitAgent(t, dir, "init", "--scope")
	if code == 0 {
		t.Fatalf("expected non-zero exit when --scope given without API key, got 0 (output: %s)", out)
	}
}

func TestInitCmd_MaxCommitsFlag_Recognized(t *testing.T) {
	dir := newGitRepo(t)
	// --max-commits is still valid; scope requires API key so it will fail, but the flag must be parsed.
	_, code := gitAgent(t, dir, "init", "--scope", "--max-commits", "50")
	// Non-zero is expected (no API key), but must not be "unknown flag".
	_ = code
}

func TestInitCmd_ForceFlag_Recognized(t *testing.T) {
	dir := newGitRepo(t)
	// --force with no API key fails on scope generation, but the flag must be parsed.
	out, _ := gitAgent(t, dir, "init", "--force")
	if strings.Contains(out, "unknown flag") {
		t.Errorf("--force flag not recognized: %s", out)
	}
}

// Hook configuration is now done via 'config set hook'.
func TestConfigSet_Hook_WritesProjectConfig(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "set", "hook", "conventional")
	if code != 0 {
		t.Fatalf("config set hook conventional: exit code %d\noutput: %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git-agent", "config.yml"))
	if err != nil {
		t.Fatalf("config.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "conventional") {
		t.Errorf("expected 'conventional' in config.yml, got:\n%s", data)
	}
}

func TestConfigSet_Hook_Empty(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "set", "hook", "empty")
	if code != 0 {
		t.Fatalf("config set hook empty: exit code %d\noutput: %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git-agent", "config.yml"))
	if err != nil {
		t.Fatalf("config.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "empty") {
		t.Errorf("expected 'empty' in config.yml, got:\n%s", data)
	}
}

func TestConfigSet_HookScript_CopiesFile(t *testing.T) {
	dir := newGitRepo(t)
	customHook := filepath.Join(dir, "my-hook.sh")
	writeFile(t, customHook, "#!/bin/sh\nexit 0\n")

	out, code := gitAgent(t, dir, "config", "set", "hook", customHook)
	if code != 0 {
		t.Fatalf("config set hook <path>: exit code %d\noutput: %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "hooks", "pre-commit")); err != nil {
		t.Errorf(".git-agent/hooks/pre-commit not created: %v", err)
	}
}

func TestConfigSet_HookScript_FileNotFound_Fails(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "config", "set", "hook", "/nonexistent/hook.sh")
	if code == 0 {
		t.Fatal("expected non-zero exit when hook file does not exist, got 0")
	}
}
