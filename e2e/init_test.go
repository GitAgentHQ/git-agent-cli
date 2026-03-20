package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCmd_DefaultBehavior_CreatesHookFile(t *testing.T) {
	dir := newGitRepo(t)
	// No flags → default to scope + empty hook. Without API key, scope is skipped
	// but hook_type should still be written to project.yml.
	// Test the hook-only path directly.
	_, code := gitAgent(t, dir, "init", "--hook-type", "empty")
	if code != 0 {
		t.Fatalf("git-agent init --hook-type empty: exit code %d", code)
	}
	projectYML := filepath.Join(dir, ".git-agent", "project.yml")
	data, err := os.ReadFile(projectYML)
	if err != nil {
		t.Fatalf("project.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "hook_type: empty") {
		t.Errorf("project.yml missing hook_type: empty, got:\n%s", data)
	}
}

func TestInitCmd_HookConventional_CreatesHook(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--hook-type", "conventional")
	if code != 0 {
		t.Fatalf("git-agent init --hook-type conventional: exit code %d", code)
	}
	projectYML := filepath.Join(dir, ".git-agent", "project.yml")
	data, err := os.ReadFile(projectYML)
	if err != nil {
		t.Fatalf("project.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "hook_type: conventional") {
		t.Errorf("project.yml missing hook_type: conventional, got:\n%s", data)
	}
}

func TestInitCmd_HookCustomPath_InstalledFromFile(t *testing.T) {
	dir := newGitRepo(t)
	customHook := filepath.Join(dir, "my-hook.sh")
	writeFile(t, customHook, "#!/bin/sh\nexit 0\n")

	_, code := gitAgent(t, dir, "init", "--hook-script", customHook)
	if code != 0 {
		t.Fatalf("git-agent init --hook-script %s: exit code %d", customHook, code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "hooks", "pre-commit")); err != nil {
		t.Errorf(".git-agent/hooks/pre-commit not created: %v", err)
	}
}

func TestInitCmd_HookCustomPath_FileNotFound_Fails(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--hook-script", "/nonexistent/hook.sh")
	if code == 0 {
		t.Fatal("expected non-zero exit when custom hook file does not exist, got 0")
	}
}

func TestInitCmd_HookTypeAndScript_MutuallyExclusive(t *testing.T) {
	dir := newGitRepo(t)
	customHook := filepath.Join(dir, "my-hook.sh")
	writeFile(t, customHook, "#!/bin/sh\nexit 0\n")

	out, code := gitAgent(t, dir, "init", "--hook-type", "conventional", "--hook-script", customHook)
	if code == 0 {
		t.Fatalf("expected non-zero exit for --hook-type + --hook-script, got 0\noutput: %s", out)
	}
}

func TestInitCmd_HookTypeLegacy_StillWorks(t *testing.T) {
	dir := newGitRepo(t)
	// Deprecated --hook flag should still work.
	_, code := gitAgent(t, dir, "init", "--hook", "conventional")
	if code != 0 {
		t.Fatalf("git-agent init --hook conventional (legacy): exit code %d", code)
	}
	projectYML := filepath.Join(dir, ".git-agent", "project.yml")
	data, err := os.ReadFile(projectYML)
	if err != nil {
		t.Fatalf("project.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "hook_type: conventional") {
		t.Errorf("project.yml missing hook_type: conventional, got:\n%s", data)
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
	// Without API key, --scope fails but --hook-type should still be attempted.
	// With the current flow, --scope is processed first and returns an error.
	out, code := gitAgent(t, dir, "init", "--scope", "--hook-type", "conventional")
	if code == 0 {
		t.Fatalf("expected non-zero exit when --scope given without API key, got 0 (output: %s)", out)
	}
}

func TestInitCmd_MaxCommitsFlag_Recognized(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--hook-type", "empty", "--max-commits", "50")
	if code != 0 {
		t.Fatalf("git-agent init --max-commits 50: exit code %d", code)
	}
}

func TestInitCmd_ForceFlag_Recognized(t *testing.T) {
	dir := newGitRepo(t)
	// First install.
	gitAgent(t, dir, "init", "--hook-type", "conventional")
	// Second install with --force.
	_, code := gitAgent(t, dir, "init", "--hook-type", "conventional", "--force")
	if code != 0 {
		t.Fatalf("git-agent init --force: exit code %d", code)
	}
}
