package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/project"
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
	localChanged := cmd.Flags().Changed("local")

	doScope, _ := cmd.Flags().GetBool("scope")
	force, _ := cmd.Flags().GetBool("force")
	maxCommits, _ := cmd.Flags().GetInt("max-commits")
	doGitignore, _ := cmd.Flags().GetBool("gitignore")
	hookValues, _ := cmd.Flags().GetStringArray("hook")

	// Default: no flags → full wizard.
	fullWizard := !scopeChanged && !gitignoreChanged && !hookChanged
	if fullWizard && localChanged {
		return fmt.Errorf("--local requires at least one action flag: --scope, --gitignore, or --hook")
	}
	if fullWizard {
		doScope = true
		doGitignore = true
	}

	// Ensure we're in a git repo before doing anything else.
	if err := ensureGitRepo(cmd); err != nil {
		return err
	}

	// Only resolve config path and check existence when we need to write config.
	needsConfig := doScope || fullWizard || hookChanged
	var configPath string
	if needsConfig {
		var err error
		configPath, err = initConfigPath(cmd)
		if err != nil {
			return err
		}
		if !force {
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf(".git-agent/config.yml already exists\nhint: use --force to reinitialize")
			}
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

func runInitScope(cmd *cobra.Command, force bool, maxCommits int, configPath string) error {
	providerCfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if providerCfg == nil || providerCfg.APIKey == "" {
		return fmt.Errorf("error: no API key configured\nhint: set --api-key flag or add api_key to ~/.config/git-agent/config.yml")
	}

	gitClient := infraGit.NewClient()

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

	fmt.Fprintf(cmd.OutOrStdout(), "scopes written to %s\n", configPath)
	return nil
}

func writeScopes(path string, scopes []project.Scope) error {
	data := map[string]any{"scopes": scopes}
	out, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshalling scopes: %w", err)
	}
	return os.WriteFile(path, out, 0644)
}

func writeHooks(configPath string, hooks []string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	return infraConfig.WriteProjectField(configPath, "hook", strings.Join(hooks, ","))
}

// ResetInitFlags resets all init command flags to their defaults.
// Required for testing since cobra retains flag values across invocations.
func ResetInitFlags() {
	initCmd.Flags().Set("scope", "false")
	initCmd.Flags().Set("gitignore", "false")
	initCmd.Flags().Set("force", "false")
	initCmd.Flags().Set("max-commits", "200")
	initCmd.Flags().Set("local", "false")
	initCmd.ResetFlags()
	initCmd.Flags().Bool("scope", false, "generate scopes via AI")
	initCmd.Flags().Bool("gitignore", false, "generate .gitignore via AI")
	initCmd.Flags().Bool("force", false, "overwrite existing config/.gitignore")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze for scope generation")
	initCmd.Flags().StringArray("hook", nil, "hook to configure: 'conventional', 'empty', or a file path (repeatable)")
	initCmd.Flags().Bool("local", false, "write config to .git-agent/config.local.yml")
}

func init() {
	initCmd.Flags().Bool("scope", false, "generate scopes via AI")
	initCmd.Flags().Bool("gitignore", false, "generate .gitignore via AI")
	initCmd.Flags().Bool("force", false, "overwrite existing config/.gitignore")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze for scope generation")
	initCmd.Flags().StringArray("hook", nil, "hook to configure: 'conventional', 'empty', or a file path (repeatable)")
	initCmd.Flags().Bool("local", false, "write config to .git-agent/config.local.yml")
	rootCmd.AddCommand(initCmd)
}
