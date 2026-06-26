package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphProvenanceCmd = &cobra.Command{
	Use:   "provenance <file>",
	Short: "Show the rename-aware change history for a file",
	Long: `Build the chronological, rename-aware history for a file from the
Event Log: every captured change (via event_files) merged with any out-of-band
changes, folding in the file's pre-rename identities. Out-of-band rows (source
"unknown") are flagged. Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: runGraphProvenance,
}

func runGraphProvenance(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
	ctx := cmd.Context()
	file := args[0]

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
	view, err := application.NewProvenanceService(repo).Provenance(ctx, file)
	if err != nil {
		return fmt.Errorf("provenance: %w", err)
	}

	out := cmd.OutOrStdout()
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, view)
	}

	fmt.Fprintf(out, "Provenance for %s (%d changes)\n", view.File, len(view.Rows))
	for _, row := range view.Rows {
		flag := ""
		if row.OutOfBand {
			flag = " [out-of-band]"
		}
		when := time.Unix(row.When, 0).UTC().Format(time.RFC3339)
		fmt.Fprintf(out, "  seq %d  %s  %s/%s  %s %s->%s%s\n",
			row.Seq, when, row.Who, row.Tool,
			row.ChangeKind, row.BeforeBlob, row.AfterBlob, flag)
		if row.LinkedCommit != "" {
			fmt.Fprintf(out, "      commit %s\n", row.LinkedCommit)
		}
	}
	return nil
}

func init() {
	graphProvenanceCmd.Flags().Bool("json", false, "emit the provenance view as JSON")
	graphProvenanceCmd.Flags().Bool("text", false, "emit the provenance view as text")
	graphProvenanceCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphProvenanceCmd)
}
