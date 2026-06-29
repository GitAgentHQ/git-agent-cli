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

var graphSymbolCmd = &cobra.Command{
	Use:   "symbol <name>",
	Short: "Show a symbol's location, signature, and caller/callee trail",
	Long: `Look up an AST symbol by name and print its kind, file, line range, and
signature, plus the one-hop caller and callee trails. The symbol's source
snippet is read from the working tree when available. Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: jsonAwareRunE(runGraphSymbol),
}

// symbolView is one matched symbol with its source snippet and one-hop trails.
type symbolView struct {
	Node    graph.ASTNode          `json:"node"`
	Source  string                 `json:"source,omitempty"`
	Callers []graph.ASTImpactEntry `json:"callers"`
	Callees []graph.ASTImpactEntry `json:"callees"`
}

// symbolResult is the JSON envelope for graph symbol (an object, not a bare
// array, so sibling fields can be added without breaking the contract).
type symbolResult struct {
	Matches []symbolView `json:"matches"`
}

func runGraphSymbol(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("reindex")
	ctx := cmd.Context()
	name := args[0]

	root, astRepo, client, err := openASTQuery(ctx, name, force, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer client.Close()

	nodes, err := astRepo.GetASTNodeBySymbol(ctx, name)
	if err != nil {
		return fmt.Errorf("lookup symbol %q: %w", name, err)
	}
	if len(nodes) == 0 {
		return symbolNotFoundHint(ctx, astRepo, name, cmd.ErrOrStderr())
	}

	reader := newSourceReader(root)
	views := make([]symbolView, 0, len(nodes))
	for _, n := range nodes {
		callers, _ := astRepo.GetCallers(ctx, n.ID, 1)
		callees, _ := astRepo.GetCallees(ctx, n.ID, 1)
		views = append(views, symbolView{Node: n, Source: reader.snippet(n), Callers: callers, Callees: callees})
	}

	out := cmd.OutOrStdout()
	if outputFormat(cmd) == output.FormatJSON {
		return output.EncodeJSON(out, symbolResult{Matches: views})
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
	graphSymbolCmd.Flags().Bool("reindex", false, "force a full AST re-index before lookup")
	graphCmd.AddCommand(graphSymbolCmd)
}
