package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/pkg/output"
)

var buildVersion = "0.7.0"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the git-agent version",
	RunE: func(cmd *cobra.Command, args []string) error {
		if outputFormat(cmd) == output.FormatJSON {
			return output.EncodeJSON(cmd.OutOrStdout(), map[string]string{"version": buildVersion})
		}
		fmt.Fprintln(cmd.OutOrStdout(), buildVersion)
		return nil
	},
}

func init() {
	rootCmd.Version = buildVersion
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	addOutputFlagWithDefault(versionCmd, false, "text")
	rootCmd.AddCommand(versionCmd)
}
