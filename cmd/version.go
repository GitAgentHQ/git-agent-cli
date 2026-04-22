package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var buildVersion = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the git-agent version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), buildVersion)
	},
}

func init() {
	rootCmd.Version = buildVersion
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
