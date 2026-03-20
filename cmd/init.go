package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fradser/git-agent/application"
	"github.com/fradser/git-agent/hooks"
	infraConfig "github.com/fradser/git-agent/infrastructure/config"
	infraGit "github.com/fradser/git-agent/infrastructure/git"
	infraOpenAI "github.com/fradser/git-agent/infrastructure/openai"
)

const defaultProjectYML = ".git-agent/project.yml"

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize git-agent in the current repository",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	scopeChanged := cmd.Flags().Changed("scope")
	hookChanged := cmd.Flags().Changed("hook")
	gitignoreChanged := cmd.Flags().Changed("gitignore")

	doScope, _ := cmd.Flags().GetBool("scope")
	hookVal, _ := cmd.Flags().GetString("hook")
	force, _ := cmd.Flags().GetBool("force")
	maxCommits, _ := cmd.Flags().GetInt("max-commits")
	doGitignore, _ := cmd.Flags().GetBool("gitignore")

	// Default: no flags → scope + empty hook + gitignore.
	if !scopeChanged && !hookChanged && !gitignoreChanged {
		doScope = true
		hookVal = "empty"
		doGitignore = true
	}

	if doScope {
		if err := runInitScope(cmd, force, maxCommits); err != nil {
			return err
		}
	}

	if hookVal != "" {
		if err := runInitHook(cmd, hookVal, force); err != nil {
			return err
		}
	}

	if doGitignore {
		if err := runGitignore(cmd.Context(), force, cmd.OutOrStdout()); err != nil {
			return err
		}
	}

	return nil
}

func runInitScope(cmd *cobra.Command, force bool, maxCommits int) error {
	providerCfg, err := infraConfig.Resolve(infraConfig.ProviderConfig{}, userConfigPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if providerCfg == nil || providerCfg.APIKey == "" {
		return fmt.Errorf("error: no API key configured\nhint: set --api-key flag or add api_key to ~/.config/git-agent/config.yml")
	}

	gitClient := infraGit.NewClient()
	if !gitClient.IsGitRepo(cmd.Context()) {
		return fmt.Errorf("not a git repository")
	}

	scopeSvc := application.NewScopeService(
		infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model),
		gitClient,
	)

	scopes, err := scopeSvc.Generate(cmd.Context(), maxCommits)
	if err != nil {
		return err
	}

	path := defaultProjectYML
	if force {
		// Overwrite: write only new scopes.
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}
		if err := writeScopes(path, scopes); err != nil {
			return err
		}
	} else {
		// Merge with existing.
		if err := scopeSvc.MergeAndSave(cmd.Context(), path, scopes); err != nil {
			return err
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "initialized git-agent in %s\n", filepath.Dir(path))
	return nil
}

func writeScopes(path string, scopes []string) error {
	var content string
	content = "scopes:\n"
	for _, s := range scopes {
		content += "  - " + s + "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func runInitHook(cmd *cobra.Command, hookVal string, force bool) error {
	hookPath := filepath.Join(filepath.Dir(defaultProjectYML), "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0755); err != nil {
		return fmt.Errorf("creating hooks dir: %w", err)
	}

	if _, err := os.Stat(hookPath); err == nil && !force {
		// Hook already exists; overwrite silently (hooks should always be current).
	}

	var hookContent []byte
	switch hookVal {
	case "conventional":
		hookContent = hooks.Conventional
	case "empty", "":
		hookContent = hooks.Empty
	default:
		// Treat as file path.
		data, err := os.ReadFile(hookVal)
		if err != nil {
			return fmt.Errorf("reading hook file %q: %w", hookVal, err)
		}
		hookContent = data
	}

	if err := os.WriteFile(hookPath, hookContent, 0755); err != nil {
		return fmt.Errorf("installing hook: %w", err)
	}

	if hookVal != "empty" && hookVal != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "installed hook: %s\n", hookVal)
	}
	return nil
}

func init() {
	initCmd.Flags().Bool("scope", false, "generate scopes via AI")
	initCmd.Flags().String("hook", "", "hook to install: conventional, empty, or path to script")
	initCmd.Flags().Bool("gitignore", false, "generate .gitignore via AI")
	initCmd.Flags().Bool("force", false, "overwrite existing config/hook/.gitignore")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze for scope generation")
	rootCmd.AddCommand(initCmd)
}
