package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestAutonomousHeartbeatWriterUsesStderr(t *testing.T) {
	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	oldVerbose := verbose
	verbose = true
	t.Cleanup(func() { verbose = oldVerbose })

	if got := autonomousHeartbeatWriter(cmd); got != cmd.ErrOrStderr() {
		t.Fatal("expected autonomous heartbeat diagnostics to use stderr")
	}
}
