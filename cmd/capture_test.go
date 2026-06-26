package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunCapture_MissingSourceReturnsError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.Flags().String("source", "", "source")
	cmd.Flags().String("tool", "", "tool")
	cmd.Flags().String("instance-id", "", "instance-id")
	cmd.Flags().String("message", "", "message")
	cmd.Flags().Bool("end-session", false, "end-session")

	err := runCapture(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing --source")
	}
	if !strings.Contains(err.Error(), "--source is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCapture_SwallowsErrorsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.Flags().String("source", "claude-code", "source")
	cmd.Flags().String("tool", "", "tool")
	cmd.Flags().String("instance-id", "", "instance-id")
	cmd.Flags().String("message", "", "message")
	cmd.Flags().Bool("end-session", false, "end-session")

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	if err := runCapture(cmd, nil); err != nil {
		t.Fatalf("expected nil error (exit 0), got: %v", err)
	}
	if !strings.Contains(stderr.String(), "capture: warning:") {
		t.Fatalf("expected warning on stderr, got: %q", stderr.String())
	}
}
