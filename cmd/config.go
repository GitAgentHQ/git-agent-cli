package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
)

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value in the specified scope.

Scopes:
  --user     ~/.config/git-agent/config.yml  (provider keys: api_key, base_url, model)
  --project  .git-agent/config.yml           (shared, checked into git)
  --local    .git-agent/config.local.yml     (personal override, gitignored)

When no scope flag is given, provider keys default to --user and all others to --project.`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Show the resolved value of a configuration key",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, err := infraConfig.ResolveKey(args[0])
	if err != nil {
		return err
	}
	rawValue := args[1]

	useUser, _ := cmd.Flags().GetBool("user")
	useProject, _ := cmd.Flags().GetBool("project")
	useLocal, _ := cmd.Flags().GetBool("local")

	var scope string
	switch {
	case useUser:
		scope = infraConfig.ScopeUser
	case useProject:
		scope = infraConfig.ScopeProject
	case useLocal:
		scope = infraConfig.ScopeLocal
	default:
		scope = infraConfig.DefaultScope(key)
	}

	if err := infraConfig.ValidateScope(key, scope); err != nil {
		return err
	}

	value, err := infraConfig.NormalizeValue(key, rawValue)
	if err != nil {
		return err
	}

	switch scope {
	case infraConfig.ScopeUser:
		if err := infraConfig.WriteUserField(userConfigPath(), key, value); err != nil {
			return fmt.Errorf("writing user config: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s  (user)\n", key, value)

	case infraConfig.ScopeProject:
		gitClient := infraGit.NewClient()
		root, err := gitClient.RepoRoot(cmd.Context())
		if err != nil {
			return fmt.Errorf("repo root: %w", err)
		}
		value, err = installHookScript(root, key, value, cmd.OutOrStdout())
		if err != nil {
			return err
		}
		path := infraConfig.ProjectConfigWritePath(root)
		if err := infraConfig.WriteProjectField(path, key, value); err != nil {
			return fmt.Errorf("writing project config: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s  (project)\n", key, value)

	case infraConfig.ScopeLocal:
		gitClient := infraGit.NewClient()
		root, err := gitClient.RepoRoot(cmd.Context())
		if err != nil {
			return fmt.Errorf("repo root: %w", err)
		}
		value, err = installHookScript(root, key, value, cmd.OutOrStdout())
		if err != nil {
			return err
		}
		path := infraConfig.LocalConfigPath(root)
		if err := infraConfig.WriteProjectField(path, key, value); err != nil {
			return fmt.Errorf("writing local config: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s  (local)\n", key, value)
	}
	return nil
}

// installHookScript handles hook values that are file paths: copies the
// script to .git-agent/hooks/pre-commit and returns the resolved absolute path.
// For built-in values ("conventional", "empty") the value is returned unchanged.
func installHookScript(repoRoot, key, value string, out interface{ Write([]byte) (int, error) }) (string, error) {
	if key != "hook" {
		return value, nil
	}
	switch value {
	case "conventional", "empty":
		return value, nil
	}
	absPath, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolving hook path %q: %w", value, err)
	}
	data, err := os.ReadFile(value)
	if err != nil {
		return "", fmt.Errorf("reading hook file %q: %w", value, err)
	}
	dest := filepath.Join(repoRoot, ".git-agent", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", fmt.Errorf("creating hooks dir: %w", err)
	}
	if err := os.WriteFile(dest, data, 0755); err != nil {
		return "", fmt.Errorf("installing hook: %w", err)
	}
	fmt.Fprintf(out, "installed hook: %s\n", value)
	return absPath, nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key, err := infraConfig.ResolveKey(args[0])
	if err != nil {
		return err
	}

	gitClient := infraGit.NewClient()
	root, _ := gitClient.RepoRoot(cmd.Context())

	value, scope, err := infraConfig.ResolveField(cmd.Context(), root, userConfigPath(), key)
	if err != nil {
		return err
	}

	if scope == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s is not set\n", key)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s = %s  (from %s)\n", key, value, scope)
	return nil
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show git-agent configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show resolved provider configuration",
	RunE:  runConfigShow,
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if infraConfig.BuildAPIKey != "" && cfg.APIKey == infraConfig.BuildAPIKey {
		fmt.Fprintln(cmd.OutOrStdout(), "mode: FREE (using built-in credentials)")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "api_key:  %s\n", maskAPIKey(cfg.APIKey))
	fmt.Fprintf(cmd.OutOrStdout(), "model:    %s\n", cfg.Model)
	fmt.Fprintf(cmd.OutOrStdout(), "base_url: %s\n", cfg.BaseURL)
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
	configSetCmd.Flags().Bool("user", false, "write to user scope (~/.config/git-agent/config.yml)")
	configSetCmd.Flags().Bool("project", false, "write to project scope (.git-agent/config.yml)")
	configSetCmd.Flags().Bool("local", false, "write to local scope (.git-agent/config.local.yml)")

	configSetCmd.MarkFlagsMutuallyExclusive("user", "project", "local")

	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	rootCmd.AddCommand(configCmd)
}
