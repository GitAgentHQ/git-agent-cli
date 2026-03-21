package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/fradser/git-agent/application"
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
	hookTypeChanged := cmd.Flags().Changed("hook-type")
	hookScriptChanged := cmd.Flags().Changed("hook-script")
	hookChanged := cmd.Flags().Changed("hook")
	gitignoreChanged := cmd.Flags().Changed("gitignore")

	doScope, _ := cmd.Flags().GetBool("scope")
	hookType, _ := cmd.Flags().GetString("hook-type")
	hookScript, _ := cmd.Flags().GetString("hook-script")
	hookLegacy, _ := cmd.Flags().GetString("hook")
	force, _ := cmd.Flags().GetBool("force")
	maxCommits, _ := cmd.Flags().GetInt("max-commits")
	doGitignore, _ := cmd.Flags().GetBool("gitignore")

	if hookTypeChanged && hookScriptChanged {
		return fmt.Errorf("--hook-type and --hook-script are mutually exclusive")
	}
	if hookTypeChanged && hookType != "conventional" && hookType != "empty" {
		return fmt.Errorf("--hook-type must be \"conventional\" or \"empty\", got %q", hookType)
	}

	// Compute resolved hook value.
	var hookVal string
	if hookTypeChanged {
		hookVal = hookType
	} else if hookScriptChanged {
		hookVal = hookScript
	} else if hookChanged {
		hookVal = hookLegacy
	}

	// Get repo root for checking existing config.
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	// Default: no flags → scope + empty hook + gitignore.
	if !scopeChanged && !hookTypeChanged && !hookScriptChanged && !hookChanged && !gitignoreChanged {
		doScope = true
		doGitignore = true

		// Only set default hook if force or no existing hook_type.
		if force || !hasExistingHookType(root) {
			hookVal = "empty"
		}
		// Otherwise preserve existing hook_type (hookVal stays empty, runInitHook skipped)
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

// runInitHook writes hook_type to project.yml.
// For "conventional" and "empty": records the type in YAML, no file copy.
// For a file path: copies the script to .git-agent/hooks/pre-commit and records the absolute path in YAML.
func runInitHook(cmd *cobra.Command, hookVal string, force bool) error {
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	ymlPath := projectYMLPath(root)
	if err := os.MkdirAll(filepath.Dir(ymlPath), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	hookTypeVal := hookVal
	switch hookVal {
	case "conventional", "empty", "":
		// built-in types — just record in YAML

	default:
		// Treat as file path: copy to .git-agent/hooks/pre-commit.
		absPath, err := filepath.Abs(hookVal)
		if err != nil {
			return fmt.Errorf("resolving hook path %q: %w", hookVal, err)
		}
		hookTypeVal = absPath

		data, err := os.ReadFile(hookVal)
		if err != nil {
			return fmt.Errorf("reading hook file %q: %w", hookVal, err)
		}
		hookDest := filepath.Join(root, ".git-agent", "hooks", "pre-commit")
		if err := os.MkdirAll(filepath.Dir(hookDest), 0755); err != nil {
			return fmt.Errorf("creating hooks dir: %w", err)
		}
		if err := os.WriteFile(hookDest, data, 0755); err != nil {
			return fmt.Errorf("installing hook: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed hook: %s\n", hookVal)
	}

	// Merge hook_type into project YAML, preserving existing keys.
	rawMap := readProjectYAMLMap(ymlPath)
	rawMap["hook_type"] = hookTypeVal

	data, err := yaml.Marshal(rawMap)
	if err != nil {
		return fmt.Errorf("marshalling yaml: %w", err)
	}
	if err := os.WriteFile(ymlPath, data, 0644); err != nil {
		return fmt.Errorf("writing project config: %w", err)
	}

	if hookVal != "empty" && hookVal != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "hook type: %s\n", hookVal)
	}
	return nil
}

func readProjectYAMLMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]any)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil || m == nil {
		return make(map[string]any)
	}
	return m
}

func hasExistingHookType(root string) bool {
	ymlPath := projectYMLPath(root)
	m := readProjectYAMLMap(ymlPath)
	_, exists := m["hook_type"]
	return exists
}

func init() {
	initCmd.Flags().Bool("scope", false, "generate scopes via AI")
	initCmd.Flags().String("hook-type", "", "built-in hook template: conventional or empty (writes hook_type to project.yml)")
	initCmd.Flags().String("hook-script", "", "path to custom hook script (copies to .git-agent/hooks/pre-commit, writes hook_type to project.yml)")
	initCmd.Flags().String("hook", "", "hook to install: conventional, empty, or path to script")
	_ = initCmd.Flags().MarkDeprecated("hook", "use --hook-type or --hook-script instead")
	initCmd.Flags().Bool("gitignore", false, "generate .gitignore via AI")
	initCmd.Flags().Bool("force", false, "overwrite existing config/hook/.gitignore")
	initCmd.Flags().Int("max-commits", 200, "max commits to analyze for scope generation")
	rootCmd.AddCommand(initCmd)
}
