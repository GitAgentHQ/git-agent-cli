package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

var graphRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild the derived projections from the Event Log",
	Long: `Verify the hash-chained Event Log, rebuild derived projections by
replaying the log, reconcile unexplained working-tree changes into out-of-band
Events, then rebuild again when any were appended.`,
	RunE: runGraphRebuild,
}

func runGraphRebuild(cmd *cobra.Command, _ []string) error {
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

	if _, err := application.SyncEventLog(ctx, repo, graphGit); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Event Log replayed: projections rebuilt")
	return nil
}

func init() {
	graphCmd.AddCommand(graphRebuildCmd)
}
