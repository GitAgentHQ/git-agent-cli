package cmd

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	gaErrors "github.com/fradser/ga-cli/pkg/errors"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:          "ga",
	Short:        "AI-first Git CLI for automated commit message generation",
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		var exitErr *gaErrors.ExitCodeError
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
}
