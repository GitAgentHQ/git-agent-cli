package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show graph index health and row counts",
	Long: `Print a snapshot of the agent graph: whether the index exists, the last
indexed commit, and row counts for commits, files, authors, co-change pairs,
sessions, and actions. Read-only.`,
	RunE: runGraphStatus,
}

func runGraphStatus(cmd *cobra.Command, _ []string) error {
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
	stats, err := repo.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("graph stats: %w", err)
	}

	out := cmd.OutOrStdout()
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, stats)
	}

	last := stats.LastIndexedCommit
	if last == "" {
		last = "(none)"
	}
	fmt.Fprintf(out, "Graph: indexed (last commit %s)\n", last)
	fmt.Fprintf(out, "  commits:    %d\n", stats.CommitCount)
	fmt.Fprintf(out, "  files:      %d\n", stats.FileCount)
	fmt.Fprintf(out, "  authors:    %d\n", stats.AuthorCount)
	fmt.Fprintf(out, "  co-change:  %d pairs\n", stats.CoChangedCount)
	fmt.Fprintf(out, "  sessions:   %d\n", stats.SessionCount)
	fmt.Fprintf(out, "  actions:    %d\n", stats.ActionCount)
	return nil
}

func init() {
	graphStatusCmd.Flags().Bool("json", false, "emit the graph stats as JSON")
	graphStatusCmd.Flags().Bool("text", false, "emit the graph stats as text")
	graphStatusCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphStatusCmd)
}
