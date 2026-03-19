package cmd_test

import (
	"testing"

	"github.com/fradser/ga-cli/cmd"
)

func TestAddCmd_specificFile(t *testing.T) {
	if err := cmd.ExecuteArgs([]string{"add", "src/main.go"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddCmd_dot(t *testing.T) {
	if err := cmd.ExecuteArgs([]string{"add", "."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
