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

func TestInitCmd_ConfigExists_BlocksWithoutForce(t *testing.T) {
	dir := t.TempDir()
	gaDir := filepath.Join(dir, ".ga")
	if err := os.MkdirAll(gaDir, 0755); err != nil {
		t.Fatal(err)
	}
	ymlPath := filepath.Join(gaDir, "project.yml")
	if err := os.WriteFile(ymlPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	err := cmd.ExecuteArgs([]string{"init", "--config", ymlPath, "--hook", "conventional"})
	requireInitRegistered(t, err)
	if err == nil {
		t.Fatal("expected error when config exists without --force, got nil")
	}
}
