package cmd_test

import (
	"strings"
	"testing"

	"github.com/fradser/git-agent/cmd"
)

func TestCommitCmd_DryRunFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"commit", "--dry-run"})
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--dry-run flag not recognized: %v", err)
	}
}

func TestCommitCmd_AllFlagRemoved(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"commit", "--all"})
	if err == nil {
		t.Fatal("expected error for removed --all flag, got nil")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected 'unknown flag' error for --all, got: %v", err)
	}
}
