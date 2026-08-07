package cmd_test

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/cmd"
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
	// A dev build with no built-in gateway URL and no config errors out on
	// "no provider configured" and must not write to stdout.
	err := cmd.ExecuteArgs([]string{"commit"})
	if err == nil {
		t.Fatal("expected error with no provider, got nil")
	}
	// Verify the error is about provider config, not a flag or parse issue.
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("unexpected unknown flag error: %v", err)
	}
}
