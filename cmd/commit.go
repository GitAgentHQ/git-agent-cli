package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/fradser/git-agent/application"
	"github.com/fradser/git-agent/domain/commit"
	"github.com/fradser/git-agent/domain/project"
	infraConfig "github.com/fradser/git-agent/infrastructure/config"
	infraGit "github.com/fradser/git-agent/infrastructure/git"
	infraHook "github.com/fradser/git-agent/infrastructure/hook"
	infraOpenAI "github.com/fradser/git-agent/infrastructure/openai"
	agentErrors "github.com/fradser/git-agent/pkg/errors"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate and create commit(s) with AI-generated messages",
	RunE:  runCommit,
}

func runCommit(cmd *cobra.Command, args []string) error {
	apiKey, _ := cmd.Flags().GetString("api-key")
	model, _ := cmd.Flags().GetString("model")
	baseURL, _ := cmd.Flags().GetString("base-url")
	intent, _ := cmd.Flags().GetString("intent")
	coAuthors, _ := cmd.Flags().GetStringArray("co-author")
	trailerFlags, _ := cmd.Flags().GetStringArray("trailer")
	noGitAgent, _ := cmd.Flags().GetBool("no-git-agent")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	maxDiffLines, _ := cmd.Flags().GetInt("max-diff-lines")

	var trailers []commit.Trailer
	for _, a := range coAuthors {
		trailers = append(trailers, commit.Trailer{Key: "Co-Authored-By", Value: a})
	}
	for _, t := range trailerFlags {
		key, value, ok := strings.Cut(t, ": ")
		if !ok {
			return fmt.Errorf("invalid --trailer format %q: expected \"Key: Value\"", t)
		}
		trailers = append(trailers, commit.Trailer{Key: key, Value: value})
	}
	if !noGitAgent {
		trailers = append(trailers, commit.Trailer{Key: "Co-Authored-By", Value: "Git Agent <noreply@git-agent.dev>"})
	}

	providerCfg, err := infraConfig.Resolve(cmd.Context(), infraConfig.ProviderConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	}, userConfigPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if providerCfg.APIKey == "" {
		return agentErrors.NewExitCodeError(1, "error: no API key configured\nhint: set --api-key flag, add api_key to ~/.config/git-agent/config.yml, or use an official release binary with a built-in key")
	}

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	projCfgPath := filepath.Join(root, ".git-agent", "project.yml")
	projCfg := loadProjectConfig(projCfgPath)

	llmClient := infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model)

	var scopeSvc *application.ScopeService
	if projCfg == nil || len(projCfg.Scopes) == 0 {
		scopeSvc = application.NewScopeService(llmClient, gitClient)
	}

	svc := application.NewCommitService(
		llmClient,
		llmClient,
		gitClient,
		infraHook.NewCompositeHookExecutor(),
		scopeSvc,
	)

	var logWriter io.Writer
	if verbose {
		logWriter = cmd.ErrOrStderr()
	}

	result, err := svc.Commit(cmd.Context(), application.CommitRequest{
		Intent:            intent,
		Trailers:          trailers,
		HookPath:          filepath.Join(root, ".git-agent", "hooks", "pre-commit"),
		DryRun:            dryRun,
		Config:            projCfg,
		MaxLines:          maxDiffLines,
		Verbose:           verbose,
		LogWriter:         logWriter,
		OutWriter:         cmd.ErrOrStderr(),
		ProjectConfigPath: projCfgPath,
	})
	if err != nil {
		if errors.Is(err, application.ErrHookBlocked) {
			return agentErrors.NewExitCodeError(2, "error: commit blocked after retries")
		}
		return err
	}

	for _, c := range result.Commits {
		if c.Outline != "" {
			fmt.Fprintln(cmd.OutOrStdout(), c.Outline)
		}
	}

	return nil
}

func userConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git-agent", "config.yml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "git-agent", "config.yml")
}

func loadProjectConfig(path string) *project.Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Scopes []string `yaml:"scopes"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return &project.Config{Scopes: raw.Scopes}
}

func init() {
	commitCmd.Flags().Bool("dry-run", false, "print commit message without committing")
	commitCmd.Flags().String("intent", "", "describe the intent of the change")
	commitCmd.Flags().StringArray("co-author", nil, "add a co-author trailer (repeatable)")
	commitCmd.Flags().StringArray("trailer", nil, "add an arbitrary git trailer, format \"Key: Value\" (repeatable)")
	commitCmd.Flags().Bool("no-git-agent", false, "omit the default Git Agent co-author trailer")
	commitCmd.Flags().String("api-key", "", "API key for the AI provider")
	commitCmd.Flags().String("model", "", "model to use for generation")
	commitCmd.Flags().String("base-url", "", "base URL for the AI provider")
	commitCmd.Flags().Int("max-diff-lines", 500, "maximum diff lines to send to the model")

	rootCmd.AddCommand(commitCmd)
}
