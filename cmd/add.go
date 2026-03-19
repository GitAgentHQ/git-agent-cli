package cmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [pathspec...]",
	Short: "Stage files for commit",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		gitArgs := append([]string{"add"}, args...)
		c := exec.CommandContext(cmd.Context(), "git", gitArgs...)
		c.Stdout = cmd.OutOrStdout()
		c.Stderr = cmd.ErrOrStderr()
		if err := c.Run(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "git add: %v\n", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
