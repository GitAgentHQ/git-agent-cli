package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCmd_DefaultBehavior_CreatesHookFile(t *testing.T) {
	dir := newGitRepo(t)
	// No flags → default to scope + empty hook. Without API key, scope is skipped
	// but hook should still be installed.
	// Use fake API key so scope fails before hook; hook installs anyway.
	// Actually: no flags default = scope + hook. Scope requires API key.
	// Test the hook-only path directly.
	_, code := gitAgent(t, dir, "init", "--hook", "empty")
	if code != 0 {
		t.Fatalf("git-agent init --hook empty: exit code %d", code)
	}
	info, err := os.Stat(filepath.Join(dir, ".git-agent", "hooks", "pre-commit"))
	if err != nil {
		t.Errorf(".git-agent/hooks/pre-commit not created: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Error("pre-commit hook is not executable")
	}
}

func TestInitCmd_HookConventional_CreatesHook(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--hook", "conventional")
	if code != 0 {
		t.Fatalf("git-agent init --hook conventional: exit code %d", code)
	}
	info, err := os.Stat(filepath.Join(dir, ".git-agent", "hooks", "pre-commit"))
	if err != nil {
		t.Errorf(".git-agent/hooks/pre-commit not created: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Error("pre-commit hook is not executable")
	}
}

func TestInitCmd_HookCustomPath_InstalledFromFile(t *testing.T) {
	dir := newGitRepo(t)
	customHook := filepath.Join(dir, "my-hook.sh")
	writeFile(t, customHook, "#!/bin/sh\nexit 0\n")

	_, code := gitAgent(t, dir, "init", "--hook", customHook)
	if code != 0 {
		t.Fatalf("git-agent init --hook %s: exit code %d", customHook, code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "hooks", "pre-commit")); err != nil {
		t.Errorf(".git-agent/hooks/pre-commit not created: %v", err)
	}
}

func TestInitCmd_HookCustomPath_FileNotFound_Fails(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--hook", "/nonexistent/hook.sh")
	if code == 0 {
		t.Fatal("expected non-zero exit when custom hook file does not exist, got 0")
	}
}

func TestInitCmd_ScopeFlag_NoAPIKey_Fails(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--scope")
	if code == 0 {
		t.Fatal("expected non-zero exit when --scope given without API key, got 0")
	}
}

func TestInitCmd_HookAndScope_BothTogether_ScopeFails(t *testing.T) {
	dir := newGitRepo(t)
	// Without API key, --scope fails but --hook should still be attempted.
	// With the current flow, --scope is processed first and returns an error.
	out, code := gitAgent(t, dir, "init", "--scope", "--hook", "conventional")
	if code == 0 {
		t.Fatalf("expected non-zero exit when --scope given without API key, got 0 (output: %s)", out)
	}
}

func TestInitCmd_MaxCommitsFlag_Recognized(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--hook", "empty", "--max-commits", "50")
	if code != 0 {
		t.Fatalf("git-agent init --max-commits 50: exit code %d", code)
	}
}

func TestInitCmd_ForceFlag_Recognized(t *testing.T) {
	dir := newGitRepo(t)
	// First install.
	gitAgent(t, dir, "init", "--hook", "conventional")
	// Second install with --force.
	_, code := gitAgent(t, dir, "init", "--hook", "conventional", "--force")
	if code != 0 {
		t.Fatalf("git-agent init --force: exit code %d", code)
	}
}
