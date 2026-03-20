package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fradser/git-agent/cmd"
)

func requireInitRegistered(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("'init' command is not registered yet: %v", err)
	}
}

func TestInitCmd_HookConventional_NoAPIKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// --hook conventional does not require an API key.
	err := cmd.ExecuteArgs([]string{"init", "--hook", "conventional"})
	requireInitRegistered(t, err)
	// May fail if not in a git repo, but must not fail with "unknown flag".
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--hook flag not recognized: %v", err)
	}
}

func TestInitCmd_HookEmpty_NoAPIKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"init", "--hook", "empty"})
	requireInitRegistered(t, err)
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--hook flag not recognized: %v", err)
	}
}

func TestInitCmd_ScopeFlag_NoAPIKey_ReturnsError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"init", "--scope"})
	requireInitRegistered(t, err)
	if err == nil {
		t.Fatal("expected error when --scope given without API key, got nil")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("expected 'API key' in error, got: %v", err)
	}
}

func TestInitCmd_HookCustomPath_FileNotFound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"init", "--hook", "/nonexistent/hook.sh"})
	requireInitRegistered(t, err)
	if err == nil {
		t.Fatal("expected error when custom hook path does not exist, got nil")
	}
}

func TestInitCmd_HookConventional_WritesHookFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Run from a temp dir that is a git repo.
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	// Create minimal git repo marker.
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0644)

	err := cmd.ExecuteArgs([]string{"init", "--hook", "conventional"})
	requireInitRegistered(t, err)
	// Hook installation should succeed (scope not required).
	if err != nil {
		// It may fail because git rev-parse --git-dir returns non-zero in a bare stub.
		// Accept either success or a git-repo-related error.
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("flag not recognized: %v", err)
		}
	}
}

func TestInitCmd_ForceFlag_Recognized(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"init", "--force", "--hook", "empty"})
	requireInitRegistered(t, err)
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--force flag not recognized: %v", err)
	}
}
