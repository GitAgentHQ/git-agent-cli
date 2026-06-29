package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Refresh Event-Log projections (automatic; kept for compatibility)",
	Long: `Bring the derived projections (sessions, actions, event_files) up to date
with the Event Log by incrementally replaying only the new events.

This is now automatic: every graph read (timeline, provenance, diagnose,
impact) syncs projections before reading, and ` + "`git-agent commit`" + ` keeps them
fresh as it writes. Run ` + "`sync`" + ` only when you want to refresh without a read —
e.g. before a scripted cold read. Read-only to the Event Log; mutates only the
derived projection tables.`,
	Hidden: true,
	RunE:   runGraphSync,
}

func runGraphSync(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
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
	graphGit := infraGit.NewGraphClient(root)

	summary, err := application.SyncIfStale(ctx, repo, graphGit)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, summary)
	}
	if summary.UpToDate {
		fmt.Fprintf(out, "Projections up to date (event seq %d)\n", summary.MaxEventSeq)
		return nil
	}
	fmt.Fprintf(out, "Synced projections to event seq %d (out-of-band appended: %d)\n",
		summary.MaxEventSeq, summary.OutOfBandAppended)
	return nil
}

func init() {
	graphSyncCmd.Flags().Bool("json", false, "emit the sync result as JSON")
	graphSyncCmd.Flags().Bool("text", false, "emit the sync result as text")
	graphSyncCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphSyncCmd)
}
