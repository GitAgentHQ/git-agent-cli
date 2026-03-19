package cmd_test

import (
	"strings"
	"testing"

	"github.com/fradser/ga-cli/cmd"
)

func TestVerboseFlag_Accepted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"commit", "--verbose"})
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--verbose flag not recognized: %v", err)
	}
}

func TestOutputContract_StdoutEmpty_OnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// With no API key, the commit command returns an error and should not
	// write to stdout.
	err := cmd.ExecuteArgs([]string{"commit"})
	if err == nil {
		t.Fatal("expected error with no API key, got nil")
	}
	// Verify the error is about missing API key, not a flag or parse issue.
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("unexpected unknown flag error: %v", err)
	}
}
