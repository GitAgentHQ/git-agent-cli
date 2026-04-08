package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose [description]",
	Short: "Trace a bug to its introducing action",
	Long:  "Combines impact analysis with action timeline to identify which agent action likely introduced a regression. (Not yet implemented -- coming in a future release.)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "diagnose is not yet implemented (planned for a future release)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(diagnoseCmd)
}
