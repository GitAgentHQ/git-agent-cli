package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fradser/ga-cli/cmd"
)

// requireInitRegistered fails the test immediately when the init subcommand is
// not yet registered, distinguishing "command not found" from a real business
// logic error.
func requireInitRegistered(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("'init' command is not registered yet: %v", err)
	}
}

func TestInitCmd_UnknownHook(t *testing.T) {
	err := cmd.ExecuteArgs([]string{"init", "--hook", "unknown-hook"})
	requireInitRegistered(t, err)
	if err == nil {
		t.Fatal("expected error for unknown hook name, got nil")
	}
}

func TestInitCmd_ConfigExists_SkipsConfigWritesHook(t *testing.T) {
	dir := t.TempDir()
	gaDir := filepath.Join(dir, ".ga")
	if err := os.MkdirAll(gaDir, 0755); err != nil {
		t.Fatal(err)
	}
	ymlPath := filepath.Join(gaDir, "project.yml")
	original := []byte("existing")
	if err := os.WriteFile(ymlPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	// Re-running init without --force should succeed: skip config, install hook.
	err := cmd.ExecuteArgs([]string{"init", "--config", ymlPath, "--hook", "conventional"})
	requireInitRegistered(t, err)
	if err != nil {
		t.Fatalf("expected no error when project.yml exists, got: %v", err)
	}

	// Config must be unchanged.
	got, _ := os.ReadFile(ymlPath)
	if string(got) != string(original) {
		t.Errorf("project.yml was overwritten; want %q, got %q", original, got)
	}

	// Hook must have been installed.
	hookPath := filepath.Join(gaDir, "hooks", "pre-commit")
	if _, err := os.Stat(hookPath); err != nil {
		t.Errorf("expected hook at %s, got: %v", hookPath, err)
	}
}
