package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphNodeCmd = &cobra.Command{
	Use:   "node <name>",
	Short: "Show a symbol's location, signature, and caller/callee trail",
	Long: `Look up an AST symbol by name and print its kind, file, line range, and
signature, plus the one-hop caller and callee trails. The symbol's source
snippet is read from the working tree when available. Drills into the AST call
graph that ` + "`impact --symbol`" + ` uses. Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: runGraphNode,
}

func runGraphNode(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
	force, _ := cmd.Flags().GetBool("reindex")
	ctx := cmd.Context()
	name := args[0]

	root, astRepo, client, err := openASTQuery(ctx, name, force, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer client.Close()

	nodes, err := astRepo.GetASTNodeByName(ctx, name)
	if err != nil {
		return fmt.Errorf("lookup symbol %q: %w", name, err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("symbol %q not found", name)
	}

	type nodeView struct {
		Node    graph.ASTNode          `json:"node"`
		Source  string                 `json:"source,omitempty"`
		Callers []graph.ASTImpactEntry `json:"callers"`
		Callees []graph.ASTImpactEntry `json:"callees"`
	}
	reader := newSourceReader(root)
	views := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		callers, _ := astRepo.GetCallers(ctx, n.ID, 1)
		callees, _ := astRepo.GetCallees(ctx, n.ID, 1)
		views = append(views, nodeView{Node: n, Source: reader.snippet(n), Callers: callers, Callees: callees})
	}

	out := cmd.OutOrStdout()
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, views)
	}
	for i, v := range views {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s  %s  %s  %s:%d-%d\n", v.Node.Name, v.Node.Kind, v.Node.Signature, v.Node.FilePath, v.Node.StartLine, v.Node.EndLine)
		if v.Node.ReturnType != "" {
			fmt.Fprintf(out, "  returns: %s\n", v.Node.ReturnType)
		}
		if v.Source != "" {
			fmt.Fprintln(out, "  source:")
			for _, line := range strings.Split(strings.TrimRight(v.Source, "\n"), "\n") {
				fmt.Fprintf(out, "    %s\n", line)
			}
		}
		fmt.Fprintf(out, "  callers (%d):\n", len(v.Callers))
		for _, c := range v.Callers {
			fmt.Fprintf(out, "    %s  %s:%d\n", c.Node.Name, c.Node.FilePath, c.Node.StartLine)
		}
		fmt.Fprintf(out, "  callees (%d):\n", len(v.Callees))
		for _, c := range v.Callees {
			fmt.Fprintf(out, "    %s  %s:%d\n", c.Node.Name, c.Node.FilePath, c.Node.StartLine)
		}
	}
	return nil
}

// sourceReader reads source-line ranges from the working tree, caching each
// file's lines so multiple nodes in the same file are read once.
type sourceReader struct {
	root  string
	cache map[string][]string
}

func newSourceReader(root string) *sourceReader {
	return &sourceReader{root: root, cache: make(map[string][]string)}
}

// snippet returns the source lines [StartLine, EndLine] of the node, or "" when
// the file or range is unavailable.
func (s *sourceReader) snippet(n graph.ASTNode) string {
	if n.FilePath == "" || n.StartLine <= 0 || n.EndLine < n.StartLine {
		return ""
	}
	lines, ok := s.cache[n.FilePath]
	if !ok {
		b, err := os.ReadFile(filepath.Join(s.root, n.FilePath))
		if err != nil {
			s.cache[n.FilePath] = nil
			return ""
		}
		lines = strings.Split(string(b), "\n")
		s.cache[n.FilePath] = lines
	}
	if lines == nil || n.StartLine > len(lines) {
		return ""
	}
	end := n.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[n.StartLine-1:end], "\n")
}

func init() {
	graphNodeCmd.Flags().Bool("reindex", false, "force a full AST re-index before lookup")
	graphNodeCmd.Flags().Bool("json", false, "emit the node view as JSON")
	graphNodeCmd.Flags().Bool("text", false, "emit the node view as text")
	graphNodeCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphNodeCmd)
}
