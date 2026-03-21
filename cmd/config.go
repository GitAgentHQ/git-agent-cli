package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show git-agent configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show resolved provider configuration",
	RunE:  runConfigShow,
}

var configScopesCmd = &cobra.Command{
	Use:   "scopes",
	Short: "List project scopes from .git-agent/project.yml",
	RunE:  runConfigScopes,
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := infraConfig.Resolve(cmd.Context(), infraConfig.ProviderConfig{}, userConfigPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if infraConfig.BuildAPIKey != "" && cfg.APIKey == infraConfig.BuildAPIKey {
		fmt.Fprintln(cmd.OutOrStdout(), "mode: FREE (using built-in credentials)")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "api-key:  %s\n", maskAPIKey(cfg.APIKey))
	fmt.Fprintf(cmd.OutOrStdout(), "model:    %s\n", cfg.Model)
	fmt.Fprintf(cmd.OutOrStdout(), "base-url: %s\n", cfg.BaseURL)
	return nil
}

func runConfigScopes(cmd *cobra.Command, args []string) error {
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		// Not in a git repo — try current directory.
		root = "."
	}

	projCfgPath := filepath.Join(root, ".git-agent", "project.yml")
	projCfg := loadProjectConfig(projCfgPath)
	if projCfg == nil || len(projCfg.Scopes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no project config found at .git-agent/project.yml")
		return nil
	}

	for _, s := range projCfg.Scopes {
		fmt.Fprintln(cmd.OutOrStdout(), s)
	}
	return nil
}

func maskAPIKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 4 {
		return "****"
	}
	return key[:4] + "****"
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configScopesCmd)
	rootCmd.AddCommand(configCmd)
}
