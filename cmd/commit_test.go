package cmd_test

import (
	"strings"
	"testing"

	"github.com/fradser/ga-cli/cmd"
)

func TestCommitCmd_DryRunFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"commit", "--dry-run"})
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--dry-run flag not recognized: %v", err)
	}
}
