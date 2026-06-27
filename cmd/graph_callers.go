package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphCallersCmd = &cobra.Command{
	Use:   "callers <symbol>",
	Short: "Show symbols that call or reference a symbol",
	Long: `Find the AST nodes that call or reference the given symbol (incoming
edges), traversed up to --depth. The inverse of ` + "`callees`" + `. Drills into
the AST call graph that ` + "`impact --symbol`" + ` uses for its structural walk.
Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: runGraphCallers,
}

func runGraphCallers(cmd *cobra.Command, args []string) error {
	return runASTNeighbors(cmd, args[0], "callers")
}

var graphCalleesCmd = &cobra.Command{
	Use:   "callees <symbol>",
	Short: "Show symbols called or referenced by a symbol",
	Long: `Find the AST nodes the given symbol calls or references (outgoing
edges), traversed up to --depth. The inverse of ` + "`callers`" + `. Drills into
the AST call graph that ` + "`impact --symbol`" + ` uses for its structural walk.
Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: runGraphCallees,
}

func runGraphCallees(cmd *cobra.Command, args []string) error {
	return runASTNeighbors(cmd, args[0], "callees")
}

// runASTNeighbors resolves symbol to its AST nodes and lists the incoming
// (callers) or outgoing (callees) edges up to --depth.
func runASTNeighbors(cmd *cobra.Command, symbol, direction string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
	depth, _ := cmd.Flags().GetInt("depth")
	force, _ := cmd.Flags().GetBool("reindex")
	ctx := cmd.Context()

	_, astRepo, client, err := openASTQuery(ctx, symbol, force, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer client.Close()

	nodes, err := astRepo.GetASTNodeBySymbol(ctx, symbol)
	if err != nil {
		return fmt.Errorf("lookup symbol %q: %w", symbol, err)
	}
	if len(nodes) == 0 {
		return symbolNotFoundHint(ctx, astRepo, symbol, cmd.ErrOrStderr())
	}

	entries := make([]graph.ASTImpactEntry, 0)
	for _, n := range nodes {
		var (
			neigh []graph.ASTImpactEntry
			gErr  error
		)
		if direction == "callers" {
			neigh, gErr = astRepo.GetCallers(ctx, n.ID, depth)
		} else {
			neigh, gErr = astRepo.GetCallees(ctx, n.ID, depth)
		}
		if gErr != nil {
			return fmt.Errorf("%s for %s: %w", direction, n.ID, gErr)
		}
		entries = append(entries, neigh...)
	}

	out := cmd.OutOrStdout()
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, map[string]any{
			"symbol":    symbol,
			"direction": direction,
			"depth":     depth,
			"results":   entries,
			"total":     len(entries),
		})
	}
	renderASTNeighbors(out, symbol, direction, entries)
	return nil
}

func renderASTNeighbors(out io.Writer, symbol, direction string, entries []graph.ASTImpactEntry) {
	if direction == "callers" {
		fmt.Fprintf(out, "Callers of %s (%d):\n", symbol, len(entries))
	} else {
		fmt.Fprintf(out, "Callees of %s (%d):\n", symbol, len(entries))
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	for _, e := range entries {
		fmt.Fprintf(out, "  %s  %s  %s:%d  (depth %d)\n",
			e.Node.Name, e.Node.Kind, e.Node.FilePath, e.Node.StartLine, e.Depth)
	}
}

func init() {
	for _, c := range []*cobra.Command{graphCallersCmd, graphCalleesCmd} {
		c.Flags().Int("depth", 1, "transitive traversal depth")
		c.Flags().Bool("reindex", false, "force a full AST re-index before query")
		c.Flags().Bool("json", false, "emit the result as JSON")
		c.Flags().Bool("text", false, "emit the result as text")
		c.MarkFlagsMutuallyExclusive("json", "text")
		graphCmd.AddCommand(c)
	}
}
