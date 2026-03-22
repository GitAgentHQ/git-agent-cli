package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize git-agent in the current repository",
	Long: `Initialize git-agent in the current repository.

With no flags, runs the full setup wizard:
  1. Ensures a git repo exists (runs 'git init' if needed)
  2. Generates .gitignore via AI
  3. Generates commit scopes from git history via AI
  4. Writes .git-agent/config.yml with scopes and hook: [conventional]

Use --scope or --gitignore to run individual steps.
Use 'git-agent config set hook <value>' to reconfigure hooks.`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	scopeChanged := cmd.Flags().Changed("scope")
	gitignoreChanged := cmd.Flags().Changed("gitignore")
	hookChanged := cmd.Flags().Changed("hook")

	useLocal, _ := cmd.Flags().GetBool("local")
	useProject, _ := cmd.Flags().GetBool("project")
	if useLocal && useProject {
		return fmt.Errorf("--project and --local are mutually exclusive")
	}

	if freeMode && (cmd.Flags().Changed("api-key") || cmd.Flags().Changed("model") || cmd.Flags().Changed("base-url")) {
		return fmt.Errorf("--free is mutually exclusive with --api-key, --model, and --base-url")
	}

	doScope, _ := cmd.Flags().GetBool("scope")
	force, _ := cmd.Flags().GetBool("force")
	maxCommits, _ := cmd.Flags().GetInt("max-commits")
	doGitignore, _ := cmd.Flags().GetBool("gitignore")
	hookValues, _ := cmd.Flags().GetStringArray("hook")

	// Default: no flags → full wizard.
	fullWizard := !scopeChanged && !gitignoreChanged && !hookChanged
	if fullWizard {
		doScope = true
		doGitignore = true
	}

	// Ensure we're in a git repo before doing anything else.
	if err := ensureGitRepo(cmd); err != nil {
		return err
	}

	configPath, err := initConfigPath(cmd)
	if err != nil {
		return err
	}

	if fullWizard && !force {
		configDir := filepath.Dir(configPath)
		if _, err := os.Stat(configDir); err == nil {
			return fmt.Errorf(".git-agent already exists in this repository\nhint: use --force to reinitialize")
		}
	}

	if doGitignore {
		if err := runGitignore(cmd, cmd.OutOrStdout()); err != nil {
			return err
		}
	}

	if doScope {
		if err := runInitScope(cmd, force, maxCommits, configPath); err != nil {
			return err
		}
	}

	// Write hooks: full wizard writes [conventional]; --hook flag writes specified values.
	if fullWizard {
		if err := writeHooks(configPath, []string{"conventional"}); err != nil {
			return err
		}
	} else if hookChanged {
		if err := writeHooks(configPath, hookValues); err != nil {
			return err
		}
	}

	return nil
}

// ensureGitRepo initializes a git repo if the current directory is not already one.
func ensureGitRepo(cmd *cobra.Command) error {
	gitClient := infraGit.NewClient()
	if gitClient.IsGitRepo(cmd.Context()) {
		return nil
	}
	c := exec.Command("git", "init")
	out, err := c.CombinedOutput()
	fmt.Fprint(cmd.OutOrStdout(), string(out))
	return err
}

// initConfigPath returns the config file path based on --local/--project flags.
func initConfigPath(cmd *cobra.Command) (string, error) {
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return "", fmt.Errorf("repo root: %w", err)
	}
	useLocal, _ := cmd.Flags().GetBool("local")
	if useLocal {
		return infraConfig.LocalConfigPath(root), nil
	}
	return infraConfig.ProjectConfigWritePath(root), nil
}

func initProviderConfig(cmd *cobra.Command) (*infraConfig.ProviderConfig, error) {
	apiKey, _ := cmd.Flags().GetString("api-key")
	model, _ := cmd.Flags().GetString("model")
	baseURL, _ := cmd.Flags().GetString("base-url")
	return infraConfig.Resolve(cmd.Context(), infraConfig.ProviderConfig{
		APIKey:   apiKey,
		Model:    model,
		BaseURL:  baseURL,
		FreeMode: freeMode,
	}, userConfigPath())
}

func runInitScope(cmd *cobra.Command, force bool, maxCommits int, configPath string) error {
	providerCfg, err := initProviderConfig(cmd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if providerCfg == nil || providerCfg.APIKey == "" {
		return fmt.Errorf("error: no API key configured\nhint: set --api-key flag or add api_key to ~/.config/git-agent/config.yml")
	}

	gitClient := infraGit.NewClient()
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

	if force {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}
		if err := writeScopes(configPath, scopes); err != nil {
			return err
		}
	} else {
		if err := scopeSvc.MergeAndSave(cmd.Context(), configPath, scopes); err != nil {
			return err
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "initialized git-agent in %s\n", filepath.Dir(root))
	return nil
}

func writeScopes(path string, scopes []string) error {
	content := "scopes:\n"
	for _, s := range scopes {
		content += "  - " + s + "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func writeHooks(configPath string, hooks []string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	return infraConfig.WriteProjectField(configPath, "hook", joinHooks(hooks))
}

func joinHooks(hooks []string) string {
	result := ""
	for i, h := range hooks {
		if i > 0 {
			result += ","
		}
		result += h
	}
	return result
}

func init() {
	initCmd.Flags().Bool("scope", false, "generate scopes via AI")
	initCmd.Flags().Bool("gitignore", false, "generate .gitignore via AI")
	initCmd.Flags().Bool("force", false, "overwrite existing config/.gitignore")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze for scope generation")
	initCmd.Flags().StringArray("hook", nil, "hook to configure: 'conventional', 'empty', or a file path (repeatable)")
	initCmd.Flags().Bool("project", false, "write config to .git-agent/config.yml (default)")
	initCmd.Flags().Bool("local", false, "write config to .git-agent/config.local.yml")
	initCmd.Flags().String("api-key", "", "API key for the AI provider")
	initCmd.Flags().String("model", "", "model to use for generation")
	initCmd.Flags().String("base-url", "", "base URL for the AI provider")
	rootCmd.AddCommand(initCmd)
}
