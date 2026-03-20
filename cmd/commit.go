package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/fradser/ga-cli/application"
	"github.com/fradser/ga-cli/domain/project"
	infraConfig "github.com/fradser/ga-cli/infrastructure/config"
	infraGit "github.com/fradser/ga-cli/infrastructure/git"
	infraHook "github.com/fradser/ga-cli/infrastructure/hook"
	infraOpenAI "github.com/fradser/ga-cli/infrastructure/openai"
	gaErrors "github.com/fradser/ga-cli/pkg/errors"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate and create a commit with an AI-generated message",
	RunE:  runCommit,
}

func runCommit(cmd *cobra.Command, args []string) error {
	apiKey, _ := cmd.Flags().GetString("api-key")
	model, _ := cmd.Flags().GetString("model")
	baseURL, _ := cmd.Flags().GetString("base-url")
	intent, _ := cmd.Flags().GetString("intent")
	coAuthor, _ := cmd.Flags().GetString("co-author")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	all, _ := cmd.Flags().GetBool("all")
	maxDiffLines, _ := cmd.Flags().GetInt("max-diff-lines")

	providerCfg, err := infraConfig.Resolve(infraConfig.ProviderConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	}, userConfigPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if providerCfg.APIKey == "" {
		return gaErrors.NewExitCodeError(1, "error: no API key configured\nhint: set --api-key flag or add api_key to ~/.config/ga/config.yml")
	}

	projCfg := loadProjectConfig()

	svc := application.NewCommitService(
		infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model),
		infraGit.NewClient(),
		infraHook.NewCompositeHookExecutor(),
	)

	var logWriter io.Writer
	if verbose {
		logWriter = cmd.ErrOrStderr()
	}

	result, err := svc.Commit(cmd.Context(), application.CommitRequest{
		Intent:    intent,
		CoAuthor:  coAuthor,
		HookPath:  ".ga/hooks/pre-commit",
		DryRun:    dryRun,
		All:       all,
		Config:    projCfg,
		MaxLines:  maxDiffLines,
		Verbose:   verbose,
		LogWriter: logWriter,
		OutWriter: cmd.ErrOrStderr(),
	})
	if err != nil {
		if errors.Is(err, application.ErrHookBlocked) {
			return gaErrors.NewExitCodeError(2, "error: commit blocked after retries")
		}
		return err
	}

	if result.Outline != "" {
		fmt.Fprintln(cmd.OutOrStdout(), result.Outline)
	}

	return nil
}

func userConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ga", "config.yml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ga", "config.yml")
}

func loadProjectConfig() *project.Config {
	data, err := os.ReadFile(".ga/project.yml")
	if err != nil {
		return &project.Config{}
	}
	var raw struct {
		Scopes []string `yaml:"scopes"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return &project.Config{}
	}
	return &project.Config{Scopes: raw.Scopes}
}

func init() {
	commitCmd.Flags().Bool("dry-run", false, "print commit message without committing")
	commitCmd.Flags().String("intent", "", "describe the intent of the change")
	commitCmd.Flags().String("co-author", "", "add a co-author to the commit message")
	commitCmd.Flags().BoolP("all", "a", false, "stage all tracked changes before committing")
	commitCmd.Flags().String("api-key", "", "API key for the AI provider")
	commitCmd.Flags().String("model", "", "model to use for generation")
	commitCmd.Flags().String("base-url", "", "base URL for the AI provider")
	commitCmd.Flags().Int("max-diff-lines", 500, "maximum diff lines to send to the model")

	rootCmd.AddCommand(commitCmd)
}
