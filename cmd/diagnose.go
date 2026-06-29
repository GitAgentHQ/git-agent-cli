package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose [symptom]",
	Short: "Trace a failing symptom to its introducing action",
	Long: `Trace a regression to the agent action that most likely introduced it.
Verifies the Event Log, derives the Suspect Window between the last passing and
first failing test Outcome Event, expands the relevant file set via co-change
impact, then ranks the suspect Events deterministically. Each Candidate carries
the before/after File Blob Refs so the introducing diff can be reconstructed.
Exits 4 on a chain integrity break unless --force.

With --llm, the top-N candidates are re-ranked by an LLM. The re-rank client is
configured via git-agent.diagnose-model / diagnose-base-url / diagnose-api-key
(each falling back to the main provider value), set with
'git-agent config set diagnose-model <value>'. The LLM may reorder but never
add candidates.`,
	Args: cobra.ArbitraryArgs,
	RunE: jsonAwareRunE(runDiagnose),
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	files, _ := cmd.Flags().GetStringSlice("file")
	useLLM, _ := cmd.Flags().GetBool("llm")
	force, _ := cmd.Flags().GetBool("force")
	topN, _ := cmd.Flags().GetInt("top")
	symptom := strings.TrimSpace(strings.Join(args, " "))

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	_, client, err := openGraphDB(ctx, root)
	if err != nil {
		return err
	}
	defer client.Close()

	repo := infraGraph.NewSQLiteRepository(client)
	// Read-side auto-sync (CQRS): capture only appends, so projections may lag.
	// Cheap no-op when current; best-effort, never blocks the diagnose read.
	graphGit := infraGit.NewGraphClient(root)
	if _, serr := application.SyncIfStale(ctx, repo, graphGit); serr != nil && verbose {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: projection sync: %v\n", serr)
	}
	impact := application.NewImpactService(repo)

	var reranker application.DiagnoseReranker
	if useLLM {
		reranker, err = buildDiagnoseReranker(cmd)
		if err != nil {
			return err
		}
	}
	// When --llm is not set, reranker stays nil and the deterministic order is
	// final (the design-allowed "no endpoint" state).
	svc := application.NewDiagnoseService(repo, impact, reranker)

	result, err := svc.Diagnose(ctx, application.DiagnoseRequest{
		Symptom: symptom,
		Files:   files,
		UseLLM:  useLLM,
		Force:   force,
		TopN:    topN,
	})
	if err != nil {
		// ErrChainIntegrity carries exit code 4; the root error mapping honors it.
		return err
	}

	out := cmd.OutOrStdout()
	if outputFormat(cmd) == output.FormatJSON {
		return output.EncodeJSON(out, result)
	}

	fmt.Fprintf(out, "Diagnose: chain_verified=%v window=seq %d..%d (%d candidates)\n",
		result.ChainVerified, result.GreenSeq, result.RedSeq, len(result.Candidates))
	if result.LowConfidence != "" {
		fmt.Fprintf(out, "  low confidence: %s\n", result.LowConfidence)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(out, "  warning: %s\n", w)
	}
	for i, c := range result.Candidates {
		fmt.Fprintf(out, "  %d. seq %d  %s  score=%.3f  %s->%s  %v\n",
			i+1, c.Seq, c.Tool, c.Score, c.BeforeBlob, c.AfterBlob, c.Files)
	}
	return nil
}

// buildDiagnoseReranker resolves provider config and constructs the LLM
// re-ranker adapter. The re-rank client uses the diagnose-specific
// model/base-url/key when set, falling back to the main provider config (so a
// user who wants a single smarter model sets only git-agent.diagnose-model).
// All four are config keys, not flags.
func buildDiagnoseReranker(cmd *cobra.Command) (application.DiagnoseReranker, error) {
	providerCfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	if providerCfg.DiagnoseAPIKey == "" {
		return nil, agentErrors.NewExitCodeError(1, "error: no API key configured for --llm\n"+
			"hint: set git-agent.diagnose-api-key (or git-agent.api-key), or use --free with a built-in key")
	}

	llmClient := infraOpenAI.NewClient(
		providerCfg.DiagnoseAPIKey,
		providerCfg.DiagnoseBaseURL,
		providerCfg.DiagnoseModel,
		providerCfg.DiagnoseTimeout,
		0, // heartbeat unused for the cold re-rank path; the default suits it
		nil,
	)
	return infraOpenAI.NewDiagnoseRerankerAdapter(llmClient), nil
}

func init() {
	diagnoseCmd.Flags().StringSlice("file", nil, "seed file(s) to anchor the relevant set")
	diagnoseCmd.Flags().Bool("llm", false, "re-rank the top candidates with the configured LLM")
	diagnoseCmd.Flags().Bool("force", false, "proceed despite an Event Log chain integrity break")
	diagnoseCmd.Flags().Int("top", 5, "number of candidates passed to the LLM re-rank")
	auditCmd.AddCommand(diagnoseCmd)
}
