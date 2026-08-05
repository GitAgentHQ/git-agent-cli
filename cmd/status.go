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
	Long: `Print a snapshot of the git-agent code graph: whether the index is built, the
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
	if graphNeedsIndex(last, stats.CommitCount) {
		// No indexed commit means the graph file exists (openGraphDB creates it
		// on first run) but nothing has been indexed yet — a never-initialized repo.
		fmt.Fprintln(out, "Graph: not indexed")
		fmt.Fprintf(out, "  db size:    %s\n", formatBytes(stats.DBSizeBytes))
		fmt.Fprintln(out, "  Run `git-agent related <file>` (or the first `git-agent commit`) to build it.")
		return nil
	}
	if last == "" {
		// Rows are committed and the last-indexed marker is written in separate
		// transactions; an interrupted run (e.g. RecomputeCoChanged failing) can
		// leave commits present with the marker unset. Keep the placeholder
		// instead of printing a blank hash.
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

func graphNeedsIndex(lastIndexedCommit string, commitCount int) bool {
	return lastIndexedCommit == "" && commitCount == 0
}

// formatBytes renders n human-readably: plain bytes below 1 KiB, otherwise the
// largest applicable binary unit (KiB, MiB, GiB, TiB) with one decimal place.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n >= div*unit && exp < 3 { // cap at TiB
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}

func init() {
	addOutputFlag(statusCmd)
	rootCmd.AddCommand(statusCmd)
}
