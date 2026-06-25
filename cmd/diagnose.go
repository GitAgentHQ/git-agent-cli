package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose [symptom]",
	Short: "Trace a failing symptom to its introducing action",
	Long: `Trace a regression to the agent action that most likely introduced it.
Verifies the Event Log, derives the Suspect Window between the last passing and
first failing test Outcome Event, expands the relevant file set via co-change
impact, then ranks the suspect Events deterministically. Each Candidate carries
the before/after File Blob Refs so the introducing diff can be reconstructed.
Exits 4 on a chain integrity break unless --force.`,
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
	// No LLM reranker is wired by default: the deterministic order is final unless
	// a re-rank backend is supplied. --llm without a backend is a no-op.
	svc := application.NewDiagnoseService(repo, impact, nil)

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

func init() {
	diagnoseCmd.Flags().StringSlice("file", nil, "seed file(s) to anchor the relevant set")
	diagnoseCmd.Flags().Bool("llm", false, "re-rank the top candidates with the configured LLM")
	diagnoseCmd.Flags().Bool("force", false, "proceed despite an Event Log chain integrity break")
	diagnoseCmd.Flags().Int("top", 5, "number of candidates passed to the LLM re-rank")
	diagnoseCmd.Flags().Bool("json", false, "emit the diagnosis result as JSON")
	rootCmd.AddCommand(diagnoseCmd)
}
