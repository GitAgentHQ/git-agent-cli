package cmd

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

var verbose bool
var freeMode bool

var rootCmd = &cobra.Command{
	Use:          "git-agent",
	Short:        "AI-first Git CLI for automated commit message generation",
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		var exitErr *agentErrors.ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}

func ExecuteArgs(args []string) error {
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&freeMode, "free", false, "use only build-time embedded credentials; ignore config file and git config")
}
