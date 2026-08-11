package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	domainProject "github.com/gitagenthq/git-agent/domain/project"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGitignore "github.com/gitagenthq/git-agent/infrastructure/gitignore"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:          "git-agent",
	Short:        "AI-first Git CLI for automated commit message generation",
	SilenceUsage: true,
	RunE:         runAutonomousRoot,
}

func Execute() {
	exitFromError(rootCmd.Execute())
}

// ExecuteContext is the signal-aware entry point used by main(). The supplied
// context is wired through to every cmd.Context() consumer (RunE handlers,
// PersistentPreRunE, etc.) so a SIGINT/SIGTERM propagates as ctx.Done()
// throughout the application and infrastructure layers.
func ExecuteContext(ctx context.Context) {
	exitFromError(rootCmd.ExecuteContext(ctx))
}

// exitFromError centralises the exit-code mapping so Execute and ExecuteContext
// agree on how errors translate to process exit codes.
func exitFromError(err error) {
	if err == nil {
		return
	}
	var exitErr *agentErrors.ExitCodeError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.Code)
	}
	os.Exit(1)
}

func ExecuteArgs(args []string) error {
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func userConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git-agent", "config.yml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git-agent", "config.yml")
}

func resolveProviderConfig(cmd *cobra.Command) (*infraConfig.ProviderConfig, error) {
	apiKey, _ := cmd.Flags().GetString("api-key")
	model, _ := cmd.Flags().GetString("model")
	baseURL, _ := cmd.Flags().GetString("base-url")
	forceFree, _ := cmd.Flags().GetBool("free")
	return infraConfig.Resolve(cmd.Context(), infraConfig.ProviderConfig{
		APIKey:           apiKey,
		Model:            model,
		BaseURL:          baseURL,
		ForceFreeGateway: forceFree,
	}, userConfigPath())
}

// providerConfigError validates the resolved provider config.
//
// Two modes:
//   - Direct: the user set an api_key, so they are calling their own endpoint
//     and must also provide base_url and model.
//   - Free shared gateway: no api_key, so requests go to the built-in (or
//     user-configured) gateway URL. The Worker pins the model server-side, so
//     model may be empty.
func providerConfigError(cfg *infraConfig.ProviderConfig) string {
	if cfg == nil {
		return "error: no provider configured\nhint: install an official release binary (free shared gateway) or set --api-key / base_url / model"
	}
	if cfg.APIKey != "" {
		if cfg.BaseURL == "" || cfg.Model == "" {
			return "error: incomplete AI provider configuration\nhint: set base_url and model in ~/.config/git-agent/config.yml or pass --base-url and --model"
		}
		return ""
	}
	if cfg.BaseURL == "" {
		return "error: no provider configured\nhint: install an official release binary (free shared gateway) or set base_url to an OpenAI-compatible endpoint"
	}
	return ""
}

func runAutonomousRoot(cmd *cobra.Command, args []string) error {
	providerCfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if configErr := providerConfigError(providerCfg); configErr != "" {
		return agentErrors.NewExitCodeError(1, configErr)
	}

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	if amend, _ := cmd.Flags().GetBool("amend"); amend {
		return runCommit(cmd, args)
	}

	allFiles, err := gitClient.AllChangedFiles(cmd.Context())
	if err != nil {
		return fmt.Errorf("listing changed files: %w", err)
	}

	if len(allFiles) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No changes detected. Repository working tree is clean.")
		return nil
	}

	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		return runCommit(cmd, args)
	}

	llmClient := infraOpenAI.NewClient(
		providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model,
		providerCfg.RequestTimeout, providerCfg.HeartbeatInterval,
		cmd.OutOrStdout(),
	)
	llmClient.SetCloudflareAIGateway(providerCfg.CloudflareAIGatewayID)

	// Ensure graph.db is untracked before inspecting files or loading DB
	_, _ = ensureGraphDBUntracked(cmd.Context(), gitClient, root)

	// 1. Autonomous check for .gitignore (create if missing, or update if missing mandatory rules)
	gitignorePath := filepath.Join(root, ".gitignore")
	existingGitignore, gitignoreErr := os.ReadFile(gitignorePath)
	if os.IsNotExist(gitignoreErr) {
		toptalClient := infraGitignore.NewToptalClient()
		gitignoreSvc := application.NewGitignoreService(
			llmClient,
			toptalClient,
			gitClient,
		)
		techs, _, err := gitignoreSvc.Generate(cmd.Context(), application.GitignoreRequest{})
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: auto-gitignore failed: %v\n", err)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Generated .gitignore (%s)\n", strings.Join(techs, ", "))
			if updatedFiles, err := gitClient.AllChangedFiles(cmd.Context()); err == nil {
				allFiles = updatedFiles
				if len(allFiles) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Working tree is clean after applying .gitignore updates.")
					return nil
				}
			}
		}
	} else if gitignoreErr == nil {
		updated := application.EnsureMandatoryIgnoreRules(string(existingGitignore))
		if updated != string(existingGitignore) {
			if err := os.WriteFile(gitignorePath, []byte(updated), 0644); err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Updated .gitignore with mandatory ignore rules.")
				if updatedFiles, err := gitClient.AllChangedFiles(cmd.Context()); err == nil {
					allFiles = updatedFiles
					if len(allFiles) == 0 {
						fmt.Fprintln(cmd.OutOrStdout(), "Working tree is clean after applying .gitignore updates.")
						return nil
					}
				}
			}
		}
	}

	// 2. Autonomous check for Scope configuration (.git-agent/config.yml)
	projCfgPath := infraConfig.ProjectConfigPath(root)
	existingScopes := application.ReadScopes(projCfgPath)
	projCfg := infraConfig.LoadProjectConfig(root, userConfigPath())

	allAvailableScopes := append([]domainProject.Scope{}, existingScopes...)
	if projCfg != nil {
		allAvailableScopes = append(allAvailableScopes, projCfg.Scopes...)
	}

	if hasUncoveredDirs(allFiles, allAvailableScopes) {
		scopeSvc := application.NewScopeService(llmClient, gitClient)
		scopes, err := scopeSvc.Generate(cmd.Context(), 200, existingScopes)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: auto-scope failed: %v\n", err)
		} else if len(scopes) > 0 {
			if added, err := scopeSvc.MergeAndSave(cmd.Context(), projCfgPath, scopes); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: save scopes failed: %v\n", err)
			} else if len(added) > 0 {
				addedNames := make([]string, len(added))
				for i, s := range added {
					addedNames[i] = s.Name
				}
				if len(existingScopes) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Updated scopes in %s (added: %s)\n", projCfgPath, strings.Join(addedNames, ", "))
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Initialized scopes in %s (%s)\n", projCfgPath, strings.Join(addedNames, ", "))
				}
			}
		}
	}

	// 3. Delegate to runCommit to execute planning and commit
	return runCommit(cmd, args)
}

var stdDirAbbrevs = map[string]string{
	"application":    "app",
	"infrastructure": "infra",
	"cmd":            "cli",
	"command":        "cli",
	"documentation":  "docs",
	"test":           "tests",
}

func hasUncoveredDirs(allFiles []string, scopes []domainProject.Scope) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, file := range allFiles {
		parts := strings.Split(filepath.ToSlash(file), "/")
		if len(parts) <= 1 {
			continue
		}
		topDir := strings.ToLower(parts[0])
		if strings.HasPrefix(topDir, ".") {
			continue
		}
		covered := false
		abbrev := stdDirAbbrevs[topDir]
		for _, s := range scopes {
			name := strings.ToLower(s.Name)
			desc := strings.ToLower(s.Description)
			if name == topDir || (abbrev != "" && name == abbrev) || strings.Contains(desc, topDir+"/") || strings.Contains(desc, topDir+" ") || desc == topDir {
				covered = true
				break
			}
		}
		if !covered {
			return true
		}
	}
	return false
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().String("api-key", "", "API key for the AI provider")
	rootCmd.PersistentFlags().String("model", "", "model to use for generation")
	rootCmd.PersistentFlags().String("base-url", "", "base URL for the AI provider")
	rootCmd.PersistentFlags().Bool("free", false, "force routing through the free shared gateway, overriding api_key / base_url / model")

	rootCmd.Flags().Bool("dry-run", false, "print commit message without committing")
	rootCmd.Flags().String("intent", "", "describe the intent of the change")
	rootCmd.Flags().StringArray("co-author", nil, "add a co-author trailer (repeatable)")
	rootCmd.Flags().StringArray("trailer", nil, "add an arbitrary git trailer, format \"Key: Value\" (repeatable)")
	rootCmd.Flags().Bool("no-attribution", false, "omit the default Git Agent co-author trailer")
	rootCmd.Flags().Bool("no-git-agent", false, "omit the default Git Agent co-author trailer")
	_ = rootCmd.Flags().MarkDeprecated("no-git-agent", "use --no-attribution instead")
	rootCmd.Flags().Bool("no-stage", false, "skip auto-staging; only commit already-staged changes")
	rootCmd.Flags().Bool("amend", false, "regenerate and amend the most recent commit")
	rootCmd.MarkFlagsMutuallyExclusive("amend", "no-stage")
	rootCmd.Flags().Int("max-diff-lines", 0, "maximum diff lines to send to the model (0 = no line limit; a byte cap always applies)")
	rootCmd.Flags().Int("max-diff-bytes", 0, "maximum diff bytes to send to the model (0 or negative = built-in default ~384 KiB; pass a positive value to override)")
	rootCmd.Flags().Int("max-plan-files", 0, "maximum file paths listed individually in the planner prompt before collapsing to directory summaries (0 or negative = built-in default 150)")
	addOutputFlagWithDefault(rootCmd, "text")
}
