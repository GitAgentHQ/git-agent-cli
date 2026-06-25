package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
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

With --llm, the top-N candidates are re-ranked by an LLM. The re-rank model is
configured via git-agent.diagnose-model (falling back to the main model); pass
--llm-model to override it for one run. The LLM may reorder but never add
candidates.`,
	Args: cobra.ArbitraryArgs,
	RunE: runDiagnose,
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	files, _ := cmd.Flags().GetStringSlice("file")
	useLLM, _ := cmd.Flags().GetBool("llm")
	force, _ := cmd.Flags().GetBool("force")
	topN, _ := cmd.Flags().GetInt("top")
	jsonFlag, _ := cmd.Flags().GetBool("json")
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
	if jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(out, "Diagnose: chain_verified=%v window=seq %d..%d (%d candidates)\n",
		result.ChainVerified, result.GreenSeq, result.RedSeq, len(result.Candidates))
	if result.LowConfidence != "" {
		fmt.Fprintf(out, "  low confidence: %s\n", result.LowConfidence)
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
// --llm-* flags override at the highest precedence.
func buildDiagnoseReranker(cmd *cobra.Command) (application.DiagnoseReranker, error) {
	providerCfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	// --llm-* overrides take precedence over the resolved diagnose-* fields,
	// which already fall back to the main provider config.
	if v, _ := cmd.Flags().GetString("llm-model"); v != "" {
		providerCfg.DiagnoseModel = v
	}
	if v, _ := cmd.Flags().GetString("llm-base-url"); v != "" {
		providerCfg.DiagnoseBaseURL = v
	}
	if v, _ := cmd.Flags().GetString("llm-api-key"); v != "" {
		providerCfg.DiagnoseAPIKey = v
	}
	if v, _ := cmd.Flags().GetDuration("llm-timeout"); v > 0 {
		providerCfg.DiagnoseTimeout = v
	}

	if providerCfg.DiagnoseAPIKey == "" {
		return nil, agentErrors.NewExitCodeError(1, "error: no API key configured for --llm\n"+
			"hint: set --llm-api-key, configure git-agent.api-key, or use --free with a built-in key")
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
	diagnoseCmd.Flags().String("llm-model", "", "model for the re-rank LLM (overrides git-agent.diagnose-model; default: the main model)")
	diagnoseCmd.Flags().String("llm-base-url", "", "base URL for the re-rank LLM (overrides git-agent.diagnose-base-url)")
	diagnoseCmd.Flags().String("llm-api-key", "", "API key for the re-rank LLM (overrides git-agent.diagnose-api-key)")
	diagnoseCmd.Flags().Duration("llm-timeout", 0, "per-attempt HTTP timeout for the re-rank LLM (default 120s)")
	diagnoseCmd.Flags().Bool("force", false, "proceed despite an Event Log chain integrity break")
	diagnoseCmd.Flags().Int("top", 5, "number of candidates passed to the LLM re-rank")
	diagnoseCmd.Flags().Bool("json", false, "emit the diagnosis result as JSON")
	rootCmd.AddCommand(diagnoseCmd)
}
