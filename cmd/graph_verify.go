package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

var graphVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check the agent Event Log for chain integrity",
	Long: `Walk the hash-chained Event Log and verify it has not been tampered
with. Recomputes each event's hash, follows the chain linkage from genesis, and
checks sequence continuity. Exits 4 on any integrity break. Read-only.`,
	RunE: runGraphVerify,
}

func runGraphVerify(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	ctx := cmd.Context()

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
	result, err := application.NewVerifyService(repo).Verify(ctx)
	if err != nil {
		return fmt.Errorf("verify chain: %w", err)
	}

	out := cmd.OutOrStdout()
	if jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "Event Log: %s (%d/%d events verified)\n",
			result.Status, result.EventsVerified, result.EventsTotal)
		if result.FirstBreak != nil {
			b := result.FirstBreak
			fmt.Fprintf(out, "First break: %s at seq %d (event %s)\n", b.Kind, b.Seq, b.EventID)
			fmt.Fprintf(out, "  expected this_hash: %s\n", b.ExpectedThisHash)
			fmt.Fprintf(out, "  stored   this_hash: %s\n", b.StoredThisHash)
		}
	}

	if result.Status == "broken" {
		return agentErrors.ErrChainIntegrity
	}
	return nil
}

func init() {
	graphVerifyCmd.Flags().Bool("json", false, "emit the verify result as JSON")
	graphCmd.AddCommand(graphVerifyCmd)
}
