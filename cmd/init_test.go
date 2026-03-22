package cmd_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/cmd"
)

func requireInitRegistered(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("'init' command is not registered yet: %v", err)
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

func TestInitCmd_ScopeFlag_APIKeyFromCLI_ReachesLLM(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	initCmd := exec.Command("git", "init")
	initCmd.Dir = dir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	os.Chdir(dir)

	err := cmd.ExecuteArgs([]string{"init", "--scope", "--api-key", "sk-invalid-key-for-test"})
	requireInitRegistered(t, err)
	if err == nil {
		t.Fatal("expected error from scope/LLM with fake key, got nil")
	}
	if strings.Contains(err.Error(), "no API key configured") {
		t.Fatalf("expected --api-key to satisfy config, got: %v", err)
	}
}

func TestInitCmd_ForceFlag_Recognized(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"init", "--force"})
	requireInitRegistered(t, err)
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--force flag not recognized: %v", err)
	}
}
