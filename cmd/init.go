package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fradser/ga-cli/application"
	"github.com/fradser/ga-cli/hooks"
	infraConfig "github.com/fradser/ga-cli/infrastructure/config"
	infraGit "github.com/fradser/ga-cli/infrastructure/git"
	infraOpenAI "github.com/fradser/ga-cli/infrastructure/openai"
)

var validHooks = map[string]bool{
	"empty":        true,
	"conventional": true,
	"commit-msg":   true,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ga in the current repository",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	hookName, _ := cmd.Flags().GetString("hook")
	if !validHooks[hookName] {
		return fmt.Errorf("unknown hook %q: must be one of empty, conventional, commit-msg", hookName)
	}

	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = ".ga/project.yml"
	}
	force, _ := cmd.Flags().GetBool("force")
	maxCommits, _ := cmd.Flags().GetInt("max-commits")

	if _, err := os.Stat(configPath); err == nil && !force {
		return fmt.Errorf("project.yml already exists at %s; use --force to overwrite", configPath)
	}

	// Attempt LLM-based scope generation if API key is available.
	providerCfg, _ := infraConfig.Resolve(infraConfig.ProviderConfig{}, userConfigPath())
	if providerCfg != nil && providerCfg.APIKey != "" {
		svc := application.NewInitService(
			infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model),
			infraGit.NewClient(),
		)
		if err := svc.Init(cmd.Context(), application.InitRequest{
			ProjectYMLPath: configPath,
			HookName:       hookName,
			Force:          force,
			MaxCommits:     maxCommits,
		}); err != nil {
			return err
		}
	} else {
		// No API key — write minimal config with empty scopes.
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}
		if err := os.WriteFile(configPath, []byte("scopes: []\n"), 0644); err != nil {
			return fmt.Errorf("writing project.yml: %w", err)
		}
	}

	// Install hook template.
	hookPath := ".ga/hooks/pre-commit"
	if err := os.MkdirAll(filepath.Dir(hookPath), 0755); err != nil {
		return fmt.Errorf("creating hooks dir: %w", err)
	}
	hookContent := hooks.Empty
	if hookName == "conventional" {
		hookContent = hooks.Conventional
	}
	if err := os.WriteFile(hookPath, hookContent, 0755); err != nil {
		return fmt.Errorf("installing hook: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "initialized ga in %s\n", filepath.Dir(configPath))
	if hookName != "" && hookName != "empty" {
		fmt.Fprintf(cmd.ErrOrStderr(), "installed hook: %s\n", hookName)
	}

	return nil
}

func init() {
	initCmd.Flags().Bool("force", false, "overwrite existing config")
	initCmd.Flags().String("hook", "empty", "hook template to install (empty, conventional, commit-msg)")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze")
	initCmd.Flags().String("config", "", "path to project.yml")
	rootCmd.AddCommand(initCmd)
}
