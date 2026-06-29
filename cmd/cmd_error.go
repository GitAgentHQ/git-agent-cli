package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
	"github.com/gitagenthq/git-agent/pkg/output"
)

// jsonAwareRunE wraps a read command's RunE so a returned error is rendered as
// the uniform JSON error envelope on stderr when the resolved output format is
// JSON. The error is returned unchanged so root's exitFromError still maps it to
// the right process exit code. With text/auto-TTY output the wrapper is a no-op
// and the human-readable error surfaces via cobra/root as before.
func jsonAwareRunE(fn func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		return renderCmdError(cmd, fn(cmd, args))
	}
}

// renderCmdError emits {"error":{"code":<code>,"message":<message>}} to stderr
// when the resolved format is JSON and err is non-nil, then returns err
// unchanged. The code is taken from an *ExitCodeError when present, else 1.
func renderCmdError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	if outputFormat(cmd) == output.FormatJSON {
		// We emit the JSON error envelope ourselves; stop cobra from also
		// printing its "Error:" line so stderr stays valid JSON for agents.
		cmd.SilenceErrors = true
		code := 1
		var ece *agentErrors.ExitCodeError
		if errors.As(err, &ece) {
			code = ece.Code
		}
		_ = output.EncodeError(cmd.ErrOrStderr(), code, err.Error())
	}
	return err
}
