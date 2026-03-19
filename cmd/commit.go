package cmd

import (
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate and create a commit with an AI-generated message",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	commitCmd.Flags().Bool("dry-run", false, "print commit message without committing")
	commitCmd.Flags().String("intent", "", "describe the intent of the change")
	commitCmd.Flags().String("co-author", "", "add a co-author to the commit message")
	commitCmd.Flags().BoolP("all", "a", false, "stage all tracked changes before committing")
	commitCmd.Flags().String("api-key", "", "API key for the AI provider")
	commitCmd.Flags().String("model", "", "model to use for generation")
	commitCmd.Flags().String("base-url", "", "base URL for the AI provider")
	commitCmd.Flags().Int("max-diff-lines", 500, "maximum diff lines to send to the model")

	rootCmd.AddCommand(commitCmd)
}
