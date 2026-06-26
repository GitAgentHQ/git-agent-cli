package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphQueryCmd = &cobra.Command{
	Use:   "query <search>",
	Short: "Search AST symbols by name or FTS5 query",
	Long: `Full-text search over indexed AST symbols (FTS5, bm25-ranked). Match by
name, qualified name, or signature. Filter by --kind (function, method, type,
etc.). The search substrate behind symbol lookups in ` + "`callers`" + ` / ` + "`callees`" + `
/ ` + "`node`" + `. Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: runGraphQuery,
}

func runGraphQuery(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
	force, _ := cmd.Flags().GetBool("reindex")
	kind, _ := cmd.Flags().GetString("kind")
	ctx := cmd.Context()
	query := args[0]

	_, astRepo, client, err := openASTQuery(ctx, "", force, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer client.Close()

	var kinds []graph.ASTNodeKind
	if kind != "" {
		kinds = []graph.ASTNodeKind{graph.ASTNodeKind(kind)}
	}
	results, err := astRepo.SearchASTNodes(ctx, query, kinds)
	if err != nil {
		return fmt.Errorf("search %q: %w", query, err)
	}

	out := cmd.OutOrStdout()
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, map[string]any{
			"query":   query,
			"results": results,
			"total":   len(results),
		})
	}
	renderSearchResults(out, query, results)
	return nil
}

func renderSearchResults(out io.Writer, query string, results []graph.ASTSearchResult) {
	fmt.Fprintf(out, "Search %q (%d):\n", query, len(results))
	if len(results) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	for _, r := range results {
		fmt.Fprintf(out, "  %s  %s  %s:%d  (score %.2f)\n",
			r.Node.Name, r.Node.Kind, r.Node.FilePath, r.Node.StartLine, r.Score)
	}
}

func init() {
	graphQueryCmd.Flags().String("kind", "", "filter by node kind (e.g. function, method, type)")
	graphQueryCmd.Flags().Bool("reindex", false, "force a full AST re-index before search")
	graphQueryCmd.Flags().Bool("json", false, "emit the search results as JSON")
	graphQueryCmd.Flags().Bool("text", false, "emit the search results as text")
	graphQueryCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphQueryCmd)
}
