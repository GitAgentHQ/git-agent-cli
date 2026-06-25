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
	Long: `Verify the hash-chained Event Log, then regenerate the derived
projections (sessions, actions, action_modifies, event_files) by replaying the
log. Refuses to rebuild on a chain integrity break. Also runs the out-of-band
reconciliation pass so working-tree changes with no capture Event are recorded.`,
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

	if _, err := application.NewReconcileService(repo, graphGit).Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}
	if err := application.NewProjectionRebuilder(repo, graphGit).Rebuild(ctx); err != nil {
		return fmt.Errorf("rebuild projections: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Event Log replayed: projections rebuilt")
	return nil
}

func init() {
	graphCmd.AddCommand(graphRebuildCmd)
}
