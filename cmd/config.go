package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	infraConfig "github.com/fradser/git-agent/infrastructure/config"
	infraGit "github.com/fradser/git-agent/infrastructure/git"
	"github.com/fradser/git-agent/infrastructure/openai"
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

var configPromptsCmd = &cobra.Command{
	Use:   "prompts",
	Short: "Print all system prompts for proxy ALLOWED_SYSTEM_PROMPTS configuration",
	Long: `Prints all static system prompts used by the CLI, delimited by "\n---\n".

Pipe the output to wrangler to sync the proxy allowlist:
  git-agent config prompts | wrangler secret put ALLOWED_SYSTEM_PROMPTS`,
	RunE: runConfigPrompts,
}

func runConfigPrompts(cmd *cobra.Command, args []string) error {
	fmt.Fprint(cmd.OutOrStdout(), strings.Join(openai.AllSystemPrompts(), "\n---\n"))
	return nil
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
	configCmd.AddCommand(configPromptsCmd)
	rootCmd.AddCommand(configCmd)
}
