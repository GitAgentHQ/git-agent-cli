package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/domain/project"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraDiff "github.com/gitagenthq/git-agent/infrastructure/diff"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	infraHook "github.com/gitagenthq/git-agent/infrastructure/hook"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
	"github.com/gitagenthq/git-agent/pkg/output"
)

// commitJSONResult is the agent-facing envelope for `commit -o json`: the
// per-commit results plus the aggregate fields an agent needs to verify the
// outcome without parsing human text.
type commitJSONResult struct {
	DryRun         bool                             `json:"dry_run"`
	Commits        []application.SingleCommitResult `json:"commits"`
	CommittedCount int                              `json:"committed_count"`
	FinalSHA       string                           `json:"final_sha,omitempty"`
}

// stderrIsTerminal reports whether os.Stderr is connected to an interactive
// terminal. Agents, pipes, and CI runners get false and therefore receive no
// phase / heartbeat output unless --verbose is set. golang.org/x/term abstracts
// over the platform-specific ioctl so this works on macOS, Linux, and Windows.
var stderrIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// commitGraphBackfillMaxCommits bounds the one-time co-change backfill the first
// commit performs when bootstrapping the graph, so a deep history cannot turn
// the first commit into a long index. Recency-weighting fades older commits, so
// a bounded recent window carries nearly all the co-change signal anyway.
const commitGraphBackfillMaxCommits = 1000

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
	maxDiffBytesFlag, _ := cmd.Flags().GetInt("max-diff-bytes")
	maxDiffBytesFlagChanged := cmd.Flags().Changed("max-diff-bytes")
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

	// Hook executor reads policy off projCfg, so fold user-scope values in.
	if providerCfg.RequireModelCoAuthor || len(providerCfg.ModelCoAuthorDomains) > 0 {
		if projCfg == nil {
			projCfg = &project.Config{}
		}
		if providerCfg.RequireModelCoAuthor {
			projCfg.RequireModelCoAuthor = true
		}
		if len(providerCfg.ModelCoAuthorDomains) > 0 {
			projCfg.ModelCoAuthorDomains = append(projCfg.ModelCoAuthorDomains, providerCfg.ModelCoAuthorDomains...)
		}
	}

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

	// Fail fast before calling the LLM: the model cannot be trusted to emit
	// the trailer correctly (wrong casing, wrong placement). Caller must
	// supply --co-author / --trailer explicitly.
	if projCfg != nil && projCfg.RequireModelCoAuthor {
		if skipCoAuthor {
			return agentErrors.NewExitCodeError(1,
				"error: require_model_co_author and no_model_co_author are both set — they contradict\nhint: unset one in your git-agent config")
		}
		domains := append([]string(nil), project.DefaultModelCoAuthorDomains...)
		domains = append(domains, projCfg.ModelCoAuthorDomains...)
		if !commit.HasModelCoAuthor(trailers, domains) {
			return agentErrors.NewExitCodeError(1, fmt.Sprintf(
				"error: require_model_co_author is enabled — pass --co-author with an email from one of: %s\n"+
					"example: git-agent commit --co-author \"Claude Opus 4.7 <noreply@anthropic.com>\"",
				strings.Join(domains, ", "),
			))
		}
	}

	// Phase output ("Planning commits...", "Drafting message: N/M...")
	// is shown when a human is watching the terminal or --verbose is set.
	// When stderr is redirected — agent subprocesses, CI runners, pipelines
	// — those lines are noise that pollutes captured logs and confuses
	// downstream parsers.
	var outWriter io.Writer
	if verbose || stderrIsTerminal() {
		outWriter = cmd.ErrOrStderr()
	}

	// The LLM heartbeat ("still waiting on LLM... (Xs elapsed)") is only
	// shown with --verbose. On a normal interactive terminal the phase
	// lines provide enough feedback; the per-tick elapsed counters add
	// clutter without actionable information.
	var heartbeatWriter io.Writer
	if verbose {
		heartbeatWriter = cmd.ErrOrStderr()
	}

	_, svc := buildCommitDeps(providerCfg, projCfg, gitClient, heartbeatWriter)

	// git-first graph generation: commit is the primary write path for the code
	// graph. Before committing, engage an EXISTING graph only — no bootstrap, so
	// nothing dirties the working tree being committed — to feed co-change hints
	// into planning and link captured actions to the commit. The graph is
	// bootstrapped and maintained AFTER the commit lands (see end of runCommit).
	graphAutobuild := projCfg == nil || projCfg.GraphAutobuild == nil || *projCfg.GraphAutobuild
	graphDBPath := infraGraph.DBPath(root)
	var graphRepo *infraGraph.SQLiteRepository
	var graphGit *infraGit.GraphClient
	if _, statErr := os.Stat(graphDBPath); statErr == nil {
		graphClient := infraGraph.NewSQLiteClient(graphDBPath)
		repo := infraGraph.NewSQLiteRepository(graphClient)
		if err := repo.Open(cmd.Context()); err == nil {
			defer repo.Close()
			if err := graphClient.ValidateSchemaVersion(cmd.Context()); err == nil {
				graphRepo = repo
				graphGit = infraGit.NewGraphClient(root)
				svc.SetCoChangeProvider(application.NewGraphCoChangeProvider(graphRepo))
			}
		}
	}

	var logWriter io.Writer
	if verbose {
		logWriter = cmd.ErrOrStderr()
	}

	maxDiffLines := maxDiffLinesFlag
	if !maxDiffLinesFlagChanged && projCfg != nil && projCfg.MaxDiffLines > 0 {
		maxDiffLines = projCfg.MaxDiffLines
	}

	maxDiffBytes := maxDiffBytesFlag
	if !maxDiffBytesFlagChanged && projCfg != nil {
		if projCfg.MaxDiffBytes > 0 {
			maxDiffBytes = projCfg.MaxDiffBytes
		} else if projCfg.MaxDiffBytes < 0 {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: max_diff_bytes %d in project config is non-positive; falling back to the built-in default\n",
				projCfg.MaxDiffBytes)
		}
	}

	result, err := svc.Commit(cmd.Context(), application.CommitRequest{
		Intent:            intent,
		Trailers:          trailers,
		DryRun:            dryRun,
		NoStage:           noStage,
		Amend:             amend,
		Config:            projCfg,
		MaxLines:          maxDiffLines,
		MaxBytes:          maxDiffBytes,
		Verbose:           verbose,
		LogWriter:         logWriter,
		OutWriter:         outWriter,
		ProjectConfigPath: projCfgPath,
	})
	if err != nil {
		// Honour SIGINT/SIGTERM cancellation before any other classification —
		// callers expect a clean "cancelled" line, not a stack of wrapped
		// error messages, when they Ctrl-C an in-flight LLM call. Check
		// cmd.Context().Err() too because net/http surfaces signal-driven
		// cancellation as an opaque signal sentinel (not context.Canceled),
		// so errors.Is alone misses the SIGINT case.
		jsonOut := outputFormat(cmd) == output.FormatJSON
		if jsonOut {
			// We emit the JSON error envelope ourselves; stop cobra from also
			// printing its "Error:" line so stderr stays valid JSON for agents.
			cmd.SilenceErrors = true
		}
		if errors.Is(err, context.Canceled) || cmd.Context().Err() != nil {
			if jsonOut {
				_ = output.EncodeError(cmd.ErrOrStderr(), 1, "cancelled")
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "cancelled")
			}
			return agentErrors.NewExitCodeError(1, "")
		}
		if jsonOut {
			// Reuse RenderCommitError to classify the exit code, but discard its
			// human text and emit the uniform JSON error envelope instead.
			mapped := RenderCommitError(io.Discard, err)
			code := 1
			msg := mapped.Error()
			var ece *agentErrors.ExitCodeError
			if errors.As(mapped, &ece) {
				code = ece.Code
				if ece.Message != "" {
					msg = ece.Message
				}
			}
			// Planner timeout/budget map to an empty ExitCodeError message (their
			// human text went to io.Discard); fall back to the underlying error so
			// the JSON envelope is never blank.
			if msg == "" {
				msg = err.Error()
			}
			_ = output.EncodeError(cmd.ErrOrStderr(), code, msg)
			return mapped
		}
		return RenderCommitError(cmd.ErrOrStderr(), err)
	}

	out := cmd.OutOrStdout()

	if outputFormat(cmd) == output.FormatJSON {
		committedCount := 0
		if !result.DryRun {
			committedCount = len(result.Commits)
		}
		finalSHA := ""
		if n := len(result.Commits); n > 0 {
			finalSHA = result.Commits[n-1].SHA
		}
		if err := output.EncodeJSON(out, commitJSONResult{
			DryRun:         result.DryRun,
			Commits:        result.Commits,
			CommittedCount: committedCount,
			FinalSHA:       finalSHA,
		}); err != nil {
			return err
		}
		// Fall through: graph autobuild below is gated on !DryRun and writes only
		// to a TTY/verbose stderr writer, so it never pollutes the JSON on stdout.
	} else if result.DryRun {
		for i, c := range result.Commits {
			fmt.Fprintf(out, "%d. %s\n   %s\n", i+1, c.Title, strings.Join(c.Files, ", "))
		}
		return nil
	} else {
		for _, c := range result.Commits {
			fmt.Fprintln(out)
			if c.GitOutput != "" {
				fmt.Fprintln(out, c.GitOutput)
			}
			if c.Explanation != "" {
				fmt.Fprintln(out, c.Explanation)
			}
		}
	}

	// git-first graph generation (continued): the commit(s) have landed and the
	// output is printed, so now bootstrap the graph if none existed and fold the
	// new commit into the co-change index — the graph grows as a byproduct of
	// committing, never needing a separate manual index step. Running here,
	// after the commit, keeps the gitignore/dir bootstrap from dirtying the
	// committed tree. Disabled by graph_autobuild=false. Strictly best-effort:
	// the commit is already done, so failures only surface under --verbose.
	if graphAutobuild && !result.DryRun {
		if graphRepo == nil {
			// First-time bootstrap does a one-time, bounded history backfill that
			// can take a moment. Announce it (only on a TTY / --verbose, like the
			// other commit phase lines) so the user isn't left at a silent prompt
			// after the commit hash already printed. Subsequent commits index
			// incrementally and print nothing.
			if outWriter != nil {
				fmt.Fprintln(outWriter, "Building code graph (first run)...")
			}
			if _, graphClient, err := openGraphDB(cmd.Context(), root); err == nil {
				defer graphClient.Close()
				graphRepo = infraGraph.NewSQLiteRepository(graphClient)
				graphGit = infraGit.NewGraphClient(root)
			} else if verbose {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: graph bootstrap: %v\n", err)
			}
		}
		if graphRepo != nil {
			idxSvc := application.NewIndexService(graphRepo, graphGit)
			ensure := application.NewEnsureIndexService(idxSvc, graphRepo, graphGit, graphDBPath)
			if _, err := ensure.EnsureIndex(cmd.Context(), graph.IndexRequest{
				MaxCommits:        commitGraphBackfillMaxCommits,
				MaxFilesPerCommit: 50,
			}); err != nil && verbose {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: graph co-change update: %v\n", err)
			}
		}
	}

	return nil
}

// RenderCommitError maps an error returned by application.CommitService.Commit
// into an exit-code-bearing error, writing any human-readable diagnostic to w.
// Cancellation (SIGINT/SIGTERM) is handled inline in runCommit because it also
// needs the live cobra context; everything else routes through here so the
// rendering logic is testable in isolation.
func RenderCommitError(w io.Writer, err error) error {
	var timeoutErr *commit.PlannerTimedOutError
	if errors.As(err, &timeoutErr) {
		fmt.Fprintf(w,
			"error: LLM planner timed out (model=%s, after %s); "+
				"try a more capable model, raise --request-timeout, "+
				"narrow scope with --intent, or split with --max-diff-lines / smaller batches\n",
			timeoutErr.Model, timeoutErr.Timeout)
		return agentErrors.NewExitCodeError(1, "")
	}
	var budgetErr *commit.PlannerBudgetExhaustedError
	if errors.As(err, &budgetErr) {
		fmt.Fprintf(w,
			"error: LLM kept producing oversized output (model=%s, ceiling=%d tokens); "+
				"try a more capable model, narrow scope with --intent, or split with "+
				"--max-diff-lines / smaller batches\n",
			budgetErr.Model, budgetErr.Ceiling)
		return agentErrors.NewExitCodeError(1, "")
	}
	var apiErr *agentErrors.APIError
	if errors.As(err, &apiErr) {
		return agentErrors.NewExitCodeError(1, apiErr.Message)
	}
	if errors.Is(err, application.ErrHookBlocked) {
		var hbe *application.HookBlockedError
		if errors.As(err, &hbe) {
			if hbe.Reason != "" {
				fmt.Fprintf(w, "\nreason: %s\n", hbe.Reason)
			}
			if hbe.LastMessage != "" {
				fmt.Fprintf(w, "\nrejected message:\n\n%s\n\n", hbe.LastMessage)
			}
		}
		fmt.Fprintf(w, "hint: use --intent \"<description>\" to guide the LLM on the next attempt\n\n")
		return agentErrors.NewExitCodeError(2, "error: commit message rejected by hook")
	}
	return err
}

// buildCommitDeps constructs the LLM client and CommitService from resolved
// config. Returning both lets callers wire scopeSvc once (it shares the same
// client) and lets tests verify that request_timeout, heartbeat_interval, and
// plan_fallback all thread through to the right collaborators.
func buildCommitDeps(
	providerCfg *infraConfig.ProviderConfig,
	projCfg *project.Config,
	gitClient *infraGit.Client,
	heartbeatOut io.Writer,
) (*infraOpenAI.Client, *application.CommitService) {
	llmClient := infraOpenAI.NewClient(
		providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model,
		providerCfg.RequestTimeout, providerCfg.HeartbeatInterval,
		heartbeatOut,
	)

	var scopeSvc *application.ScopeService
	if projCfg == nil || len(projCfg.Scopes) == 0 {
		scopeSvc = application.NewScopeService(llmClient, gitClient)
	}

	// Heuristic planner is wired unless the project explicitly opts out via
	// plan_fallback=none. The application layer guards the fallback path on
	// the same config — the bucketer is always available as a collaborator,
	// and the policy decision lives in one place (CommitService.runPlan).
	var heuristicPlanner commit.HeuristicPlanner
	if projCfg == nil || projCfg.PlanFallback != project.PlanFallbackNone {
		heuristicPlanner = application.NewDirectoryBucketer()
	}

	svc := application.NewCommitService(
		llmClient,
		llmClient,
		gitClient,
		infraHook.NewCompositeHookExecutor(),
		scopeSvc,
		infraDiff.NewPatternFilter(),
		infraDiff.NewLineTruncator(),
		heuristicPlanner,
	)
	return llmClient, svc
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
	commitCmd.Flags().Int("max-diff-lines", 0, "maximum diff lines to send to the model (0 = no line limit; a byte cap always applies)")
	commitCmd.Flags().Int("max-diff-bytes", 0, "maximum diff bytes to send to the model (0 or negative = built-in default ~384 KiB; pass a positive value to override)")
	commitCmd.MarkFlagsMutuallyExclusive("amend", "no-stage")
	addOutputFlagWithDefault(commitCmd, false, "text")

	rootCmd.AddCommand(commitCmd)
}
