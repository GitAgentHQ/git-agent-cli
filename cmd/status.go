package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show git-agent index health and row counts",
	Long: `Print a snapshot of the git-agent code graph: whether the index exists, the
last indexed commit, row counts for commits, files, authors, and co-change
pairs, and the database file size. Read-only.`,
	RunE: jsonAwareRunE(runStatus),
}

func runStatus(cmd *cobra.Command, _ []string) error {
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
	if outputFormat(cmd) == output.FormatJSON {
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
	fmt.Fprintf(out, "  db size:    %s\n", formatBytes(stats.DBSizeBytes))
	return nil
}

// formatBytes renders n human-readably: plain bytes below 1 KiB, otherwise
// KiB or MiB with one decimal place.
func formatBytes(n int64) string {
	const unit = 1024
	switch {
	case n < unit:
		return fmt.Sprintf("%d B", n)
	case n < unit*unit:
		return fmt.Sprintf("%.1f KiB", float64(n)/unit)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(unit*unit))
	}
}

func init() {
	addOutputFlag(statusCmd)
	rootCmd.AddCommand(statusCmd)
}
