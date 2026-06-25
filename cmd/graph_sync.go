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
	Short: "Bring projections up to date with the Event Log",
	Long: `Bring the derived projections (sessions, actions, event_files, co-change)
up to date with the Event Log. If the projections already reflect the latest
event seq this is a no-op; otherwise the cold path runs (replay + reconcile).
Lighter than ` + "`rebuild`" + `, which always resets and replays the whole log.
Read-only to the Event Log; mutates only the derived projection tables.`,
	RunE: runGraphSync,
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

	maxEvent, err := repo.MaxEventSeq(ctx)
	if err != nil {
		return fmt.Errorf("max event seq: %w", err)
	}
	maxProjected, err := repo.MaxProjectedEventSeq(ctx)
	if err != nil {
		return fmt.Errorf("max projected seq: %w", err)
	}

	upToDate := maxProjected >= maxEvent
	var res application.ReconcileResult
	if !upToDate {
		res, err = application.SyncEventLog(ctx, repo, graphGit)
		if err != nil {
			return err
		}
	}

	out := cmd.OutOrStdout()
	summary := map[string]any{
		"max_event_seq":        maxEvent,
		"max_projected_seq":    maxProjected,
		"up_to_date":           upToDate,
		"out_of_band_appended": res.OutOfBandAppended,
	}
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, summary)
	}
	if upToDate {
		fmt.Fprintf(out, "Projections up to date (event seq %d)\n", maxEvent)
		return nil
	}
	fmt.Fprintf(out, "Synced projections to event seq %d (out-of-band appended: %d)\n", maxEvent, res.OutOfBandAppended)
	return nil
}

func init() {
	graphSyncCmd.Flags().Bool("json", false, "emit the sync result as JSON")
	graphSyncCmd.Flags().Bool("text", false, "emit the sync result as text")
	graphSyncCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphSyncCmd)
}
