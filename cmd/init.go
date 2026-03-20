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

func projectYMLPath(root string) string {
	return filepath.Join(root, ".git-agent", "project.yml")
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize git-agent in the current repository",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	scopeChanged := cmd.Flags().Changed("scope")
	hookChanged := cmd.Flags().Changed("hook")
	gitignoreChanged := cmd.Flags().Changed("gitignore")
	installHookChanged := cmd.Flags().Changed("install-hook")

	doScope, _ := cmd.Flags().GetBool("scope")
	hookVal, _ := cmd.Flags().GetString("hook")
	force, _ := cmd.Flags().GetBool("force")
	maxCommits, _ := cmd.Flags().GetInt("max-commits")
	doGitignore, _ := cmd.Flags().GetBool("gitignore")
	doInstallHook, _ := cmd.Flags().GetBool("install-hook")

	// Default: no flags → scope + empty hook + gitignore.
	if !scopeChanged && !hookChanged && !gitignoreChanged && !installHookChanged {
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

	if doInstallHook {
		if err := runInstallHook(cmd, force); err != nil {
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
	providerCfg, err := infraConfig.Resolve(cmd.Context(), infraConfig.ProviderConfig{}, userConfigPath())
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

	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	scopeSvc := application.NewScopeService(
		infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model),
		gitClient,
	)

	scopes, err := scopeSvc.Generate(cmd.Context(), maxCommits)
	if err != nil {
		return err
	}

	path := projectYMLPath(root)
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
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	hookPath := filepath.Join(root, ".git-agent", "hooks", "pre-commit")
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

func runInstallHook(cmd *cobra.Command, force bool) error {
	gitClient := infraGit.NewClient()
	hooksDir, err := gitClient.HooksPath(cmd.Context())
	if err != nil {
		return fmt.Errorf("hooks path: %w", err)
	}

	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("creating hooks dir: %w", err)
	}

	shimDest := filepath.Join(hooksDir, "commit-msg")
	if _, err := os.Stat(shimDest); err == nil {
		if !force {
			return fmt.Errorf("commit-msg hook already exists; use --force to overwrite")
		}
		// Back up the existing hook before overwriting.
		backup := shimDest + ".pre-git-agent"
		if err := os.Rename(shimDest, backup); err != nil {
			return fmt.Errorf("backing up existing hook: %w", err)
		}
	}

	if err := os.WriteFile(shimDest, hooks.Shim, 0755); err != nil {
		return fmt.Errorf("installing commit-msg shim: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "installed commit-msg shim: %s\n", shimDest)
	return nil
}

func init() {
	initCmd.Flags().Bool("scope", false, "generate scopes via AI")
	initCmd.Flags().String("hook", "", "hook to install: conventional, empty, or path to script")
	initCmd.Flags().Bool("gitignore", false, "generate .gitignore via AI")
	initCmd.Flags().Bool("force", false, "overwrite existing config/hook/.gitignore")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze for scope generation")
	initCmd.Flags().Bool("install-hook", false, "install a commit-msg shim into .git/hooks/ that delegates to git-agent")
	rootCmd.AddCommand(initCmd)
}
