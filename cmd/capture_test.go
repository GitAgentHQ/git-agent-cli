package cmd

import (
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
