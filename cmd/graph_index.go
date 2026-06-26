package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

var graphIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Build the derived indexes from the Event Log and working tree",
	Long: `Build (or refresh) all derived indexes:
  1. Verify the hash-chained Event Log, then rebuild the event-log projections
     (sessions, actions, event_files, co-change) by replaying it, reconciling
     unexplained working-tree changes into out-of-band Events, and replaying
     again when any were appended.
  2. Ensure the AST index is fresh against the current working tree (the symbol
     and call-graph index that graph impact --symbol / callers / callees / node
     / query / affected read). Use --reindex to force a full AST re-index.

This is the codegraph ` + "`index`" + ` analogue: one command that builds every
derived store. Mutates only derived tables; the append-only Event Log is never
touched.`,
	RunE: runGraphIndex,
}

func runGraphIndex(cmd *cobra.Command, _ []string) error {
	reindex, _ := cmd.Flags().GetBool("reindex")
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

	// Ensure the AST index is fresh (full re-index when --reindex). stateRepo is
	// the same *SQLiteRepository, which implements ASTIndexStateRepository. The
	// AST index keys off the committed HEAD, so skip it on a repo with no
	// commits yet (nothing tracked to index).
	astRepo := infraGraph.NewSQLiteASTRepository(client)
	if _, headErr := graphGit.CurrentHead(ctx); headErr != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "AST index skipped: repo has no commits yet")
	} else if err := ensureASTIndex(ctx, root, astRepo, repo, graphGit, "", reindex, cmd.ErrOrStderr()); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Event Log replayed: projections rebuilt; AST index ensured")
	return nil
}

func init() {
	graphIndexCmd.Flags().Bool("reindex", false, "force a full AST re-index")
	graphCmd.AddCommand(graphIndexCmd)
}
