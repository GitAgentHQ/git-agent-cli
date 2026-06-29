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
	Short: "Rebuild Event-Log projections and the AST index (use init --graph for a full build)",
	Long: `Rebuild the Event-Log projections (sessions, actions, event_files) by
replaying the Event Log, reconciling unexplained working-tree changes into
out-of-band Events, and replaying again when any were appended; then ensure the
AST index is fresh against the current working tree. Use --reindex to force a
full AST re-index.

Note: this does NOT rebuild the commit-history co-change index (that layer is
maintained by ` + "`git-agent commit`" + ` and the ` + "`impact`" + ` read path). For a one-shot
full build of ALL three layers, use ` + "`git-agent init --graph`" + `.

Hidden because building is now automatic: ` + "`commit`" + ` maintains the graph as it
writes, and every graph read syncs projections first. Kept as a compatibility
alias for scripts.`,
	Hidden: true,
	RunE:   runGraphIndex,
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
