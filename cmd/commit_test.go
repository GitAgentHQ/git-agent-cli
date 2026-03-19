package cmd_test

import (
	"testing"

	"github.com/fradser/ga-cli/cmd"
)

func TestCommitCmd_DryRunFlag(t *testing.T) {
	if err := cmd.ExecuteArgs([]string{"commit", "--dry-run"}); err != nil {
		t.Fatalf("unexpected error from flag parsing: %v", err)
	}
}
