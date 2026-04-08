package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraDiff "github.com/gitagenthq/git-agent/infrastructure/diff"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	infraHook "github.com/gitagenthq/git-agent/infrastructure/hook"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
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
	intent, _ := cmd.Flags().GetString("intent")
	coAuthors, _ := cmd.Flags().GetStringArray("co-author")
	trailerFlags, _ := cmd.Flags().GetStringArray("trailer")
	noGitAgentCoAuthor, _ := cmd.Flags().GetBool("no-attribution")
	noGitAgentLegacy, _ := cmd.Flags().GetBool("no-git-agent")
	noGitAgent := noGitAgentCoAuthor || noGitAgentLegacy
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noStage, _ := cmd.Flags().GetBool("no-stage")
	amend, _ := cmd.Flags().GetBool("amend")
	maxDiffLinesFlag, _ := cmd.Flags().GetInt("max-diff-lines")
	maxDiffLinesFlagChanged := cmd.Flags().Changed("max-diff-lines")
	providerCfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	providerCfg.NoGitAgentCoAuthor = providerCfg.NoGitAgentCoAuthor || noGitAgent

	if providerCfg.APIKey == "" {
		return agentErrors.NewExitCodeError(1, "error: no API key configured\nhint: set --api-key flag, add api_key to ~/.config/git-agent/config.yml, or use an official release binary with a built-in key")
	}

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	projCfgPath := infraConfig.ProjectConfigPath(root)
	projCfg := infraConfig.LoadProjectConfig(root, userConfigPath())

	skipCoAuthor := providerCfg.NoModelCoAuthor || (projCfg != nil && projCfg.NoModelCoAuthor)
	var trailers []commit.Trailer
	if !skipCoAuthor {
		for _, a := range coAuthors {
			trailers = append(trailers, commit.Trailer{Key: "Co-Authored-By", Value: a})
		}
	}
	for _, t := range trailerFlags {
		key, value, ok := strings.Cut(t, ": ")
		if !ok {
			return fmt.Errorf("invalid --trailer format %q: expected \"Key: Value\"", t)
		}
		trailers = append(trailers, commit.Trailer{Key: key, Value: value})
	}
	skipAttribution := providerCfg.NoGitAgentCoAuthor || (projCfg != nil && projCfg.NoGitAgentCoAuthor)
	if !skipAttribution {
		trailers = append(trailers, commit.Trailer{Key: "Co-Authored-By", Value: "Git Agent <noreply@git-agent.dev>"})
	}

	llmClient := infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model)

	var scopeSvc *application.ScopeService
	if projCfg == nil || len(projCfg.Scopes) == 0 {
		scopeSvc = application.NewScopeService(llmClient, gitClient)
	}

	// Only create co-change provider if graph.db exists (don't force indexing during commit).
	var coChangeProvider application.CoChangeProvider
	graphDBPath := filepath.Join(root, ".git-agent", "graph.db")
	var actionLinker application.ActionLinker
	if _, err := os.Stat(graphDBPath); err == nil {
		graphClient := infraGraph.NewSQLiteClient(graphDBPath)
		graphRepo := infraGraph.NewSQLiteRepository(graphClient)
		if err := graphRepo.Open(cmd.Context()); err == nil {
			defer graphRepo.Close()
			coChangeProvider = application.NewGraphCoChangeProvider(graphRepo)
			actionLinker = application.NewGraphActionLinker(graphRepo)
		}
	}

	svc := application.NewCommitService(
		llmClient,
		llmClient,
		gitClient,
		infraHook.NewCompositeHookExecutor(),
		scopeSvc,
		infraDiff.NewPatternFilter(),
		infraDiff.NewLineTruncator(),
		coChangeProvider,
	)
	if actionLinker != nil {
		svc.SetActionLinker(actionLinker)
	}

	var logWriter io.Writer
	if verbose {
		logWriter = cmd.ErrOrStderr()
	}

	maxDiffLines := maxDiffLinesFlag
	if !maxDiffLinesFlagChanged && projCfg != nil && projCfg.MaxDiffLines > 0 {
		maxDiffLines = projCfg.MaxDiffLines
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
		var apiErr *agentErrors.APIError
		if errors.As(err, &apiErr) {
			return agentErrors.NewExitCodeError(1, apiErr.Message)
		}
		if errors.Is(err, application.ErrHookBlocked) {
			var hbe *application.HookBlockedError
			if errors.As(err, &hbe) {
				if hbe.Reason != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "\nhook rejected: %s\n", hbe.Reason)
				}
				if hbe.LastMessage != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "\nrejected message:\n\n%s\n\n", hbe.LastMessage)
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "hint: use --intent \"<description>\" to guide the next attempt\n\n")
			return agentErrors.NewExitCodeError(2, "error: commit blocked after retries")
		}
		return err
	}

	out := cmd.OutOrStdout()

	if result.DryRun {
		for i, c := range result.Commits {
			fmt.Fprintf(out, "%d. %s\n   %s\n", i+1, c.Title, strings.Join(c.Files, ", "))
		}
		return nil
	}

	for _, c := range result.Commits {
		fmt.Fprintln(out)
		if c.GitOutput != "" {
			fmt.Fprintln(out, c.GitOutput)
		}
		if c.Explanation != "" {
			fmt.Fprintln(out, c.Explanation)
		}
	}

	return nil
}

func init() {
	commitCmd.Flags().Bool("dry-run", false, "print commit message without committing")
	commitCmd.Flags().String("intent", "", "describe the intent of the change")
	commitCmd.Flags().StringArray("co-author", nil, "add a co-author trailer (repeatable)")
	commitCmd.Flags().StringArray("trailer", nil, "add an arbitrary git trailer, format \"Key: Value\" (repeatable)")
	commitCmd.Flags().Bool("no-attribution", false, "omit the default Git Agent co-author trailer")
	commitCmd.Flags().Bool("no-git-agent", false, "omit the default Git Agent co-author trailer")
	_ = commitCmd.Flags().MarkDeprecated("no-git-agent", "use --no-attribution instead")
	commitCmd.Flags().Bool("no-stage", false, "skip auto-staging; only commit already-staged changes")
	commitCmd.Flags().Bool("amend", false, "regenerate and amend the most recent commit")
	commitCmd.Flags().Int("max-diff-lines", 0, "maximum diff lines to send to the model (0 = no limit)")
	commitCmd.MarkFlagsMutuallyExclusive("amend", "no-stage")

	rootCmd.AddCommand(commitCmd)
}
