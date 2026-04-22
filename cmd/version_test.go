package cmd_test

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/cmd"
)

func TestVersionFlag_Accepted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"--version"})
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--version flag not recognized: %v", err)
	}
	if err != nil {
		t.Fatalf("--version returned error: %v", err)
	}
}
