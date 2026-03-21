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
	infraDiff "github.com/fradser/git-agent/infrastructure/diff"
	infraGit "github.com/fradser/git-agent/infrastructure/git"
	infraHook "github.com/fradser/git-agent/infrastructure/hook"
	infraOpenAI "github.com/fradser/git-agent/infrastructure/openai"
	agentErrors "github.com/fradser/git-agent/pkg/errors"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate and create commit(s) with AI-generated messages",
	Long: `Generate and create commit(s) with AI-generated messages.

Configuration resolution (highest to lowest priority):
  1. CLI flags (--api-key, --model, --base-url)
  2. git config --local git-agent.{model,base-url}
  3. ~/.config/git-agent/config.yml (supports $ENV_VAR expansion)
  4. Build-time defaults`,
	RunE: runCommit,
}

func runCommit(cmd *cobra.Command, args []string) error {
	apiKey, _ := cmd.Flags().GetString("api-key")
	model, _ := cmd.Flags().GetString("model")
	baseURL, _ := cmd.Flags().GetString("base-url")
	intent, _ := cmd.Flags().GetString("intent")
	coAuthors, _ := cmd.Flags().GetStringArray("co-author")
	trailerFlags, _ := cmd.Flags().GetStringArray("trailer")
	noAttribution, _ := cmd.Flags().GetBool("no-attribution")
	noGitAgentLegacy, _ := cmd.Flags().GetBool("no-git-agent")
	noGitAgent := noAttribution || noGitAgentLegacy
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noStage, _ := cmd.Flags().GetBool("no-stage")
	amend, _ := cmd.Flags().GetBool("amend")
	maxDiffLines, _ := cmd.Flags().GetInt("max-diff-lines")
	free, _ := cmd.Flags().GetBool("free")

	if amend && noStage {
		return fmt.Errorf("--amend and --no-stage are mutually exclusive")
	}

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

	// When --free is set, ignore config file, git config, and build-time defaults.
	// Only use CLI flags or hardcoded defaults.
	cfgPath := userConfigPath()
	if free {
		cfgPath = ""
	}

	providerCfg, err := infraConfig.Resolve(cmd.Context(), infraConfig.ProviderConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
		FreeMode: free,
	}, cfgPath)
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
		infraDiff.NewPatternFilter(),
		infraDiff.NewLineTruncator(),
	)

	var logWriter io.Writer
	if verbose {
		logWriter = cmd.ErrOrStderr()
	}

	result, err := svc.Commit(cmd.Context(), application.CommitRequest{
		Intent:            intent,
		Trailers:          trailers,
		DryRun:            dryRun,
		NoStage:           noStage,
		Amend:             amend,
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

	if result.DryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "dry-run: %d commit(s) planned, nothing committed\n", len(result.Commits))
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
		Scopes   []string `yaml:"scopes"`
		HookType string   `yaml:"hook_type"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return &project.Config{Scopes: raw.Scopes, HookType: raw.HookType}
}

func init() {
	commitCmd.Flags().Bool("dry-run", false, "print commit message without committing")
	commitCmd.Flags().String("intent", "", "describe the intent of the change")
	commitCmd.Flags().StringArray("co-author", nil, "add a co-author trailer (repeatable)")
	commitCmd.Flags().StringArray("trailer", nil, "add an arbitrary git trailer, format \"Key: Value\" (repeatable)")
	commitCmd.Flags().Bool("no-attribution", false, "omit the default Git Agent co-author trailer")
	commitCmd.Flags().Bool("no-git-agent", false, "")
	_ = commitCmd.Flags().MarkDeprecated("no-git-agent", "use --no-attribution instead")
	commitCmd.Flags().Bool("no-stage", false, "skip auto-staging; only commit already-staged changes")
	commitCmd.Flags().Bool("amend", false, "regenerate and amend the most recent commit")
	commitCmd.Flags().String("api-key", "", "API key for the AI provider")
	commitCmd.Flags().String("model", "", "model to use for generation")
	commitCmd.Flags().String("base-url", "", "base URL for the AI provider")
	commitCmd.Flags().Int("max-diff-lines", 0, "maximum diff lines to send to the model (0 = no limit)")
	commitCmd.Flags().Bool("free", false, "ignore config file, git config, and build-time defaults; use only CLI flags or hardcoded defaults")

	rootCmd.AddCommand(commitCmd)
}
