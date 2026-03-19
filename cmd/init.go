package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var validHooks = map[string]bool{
	"empty":        true,
	"conventional": true,
	"commit-msg":   true,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ga in the current repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		hook, _ := cmd.Flags().GetString("hook")
		if !validHooks[hook] {
			return fmt.Errorf("unknown hook %q: must be one of empty, conventional, commit-msg", hook)
		}

		configPath, _ := cmd.Flags().GetString("config")
		force, _ := cmd.Flags().GetBool("force")

		if configPath != "" {
			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("project.yml already exists at %s; use --force to overwrite", configPath)
			}
		}

		return nil
	},
}

func init() {
	initCmd.Flags().Bool("force", false, "overwrite existing config")
	initCmd.Flags().String("hook", "empty", "hook template to install (empty, conventional, commit-msg)")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze")
	initCmd.Flags().String("config", "", "path to project.yml")
	rootCmd.AddCommand(initCmd)
}
