package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search code symbols by name or FTS5 query",
	Long: `Full-text search over indexed AST symbols (FTS5, bm25-ranked). Match by
name, qualified name, or signature. Filter by --kind (function, method, type,
etc.). The search substrate behind symbol lookups in ` + "`callers`" + ` / ` + "`callees`" + `
/ ` + "`symbol`" + `. Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: jsonAwareRunE(runGraphSearch),
}

// searchResult is the JSON envelope for graph search.
type searchResult struct {
	Query   string                  `json:"query"`
	Results []graph.ASTSearchResult `json:"results"`
	Total   int                     `json:"total"`
}

func runGraphSearch(cmd *cobra.Command, args []string) error {
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
	if outputFormat(cmd) == output.FormatJSON {
		return output.EncodeJSON(out, searchResult{Query: query, Results: results, Total: len(results)})
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
	graphSearchCmd.Flags().String("kind", "", "filter by node kind (e.g. function, method, type)")
	graphSearchCmd.Flags().Bool("reindex", false, "force a full AST re-index before search")
	graphCmd.AddCommand(graphSearchCmd)
}
