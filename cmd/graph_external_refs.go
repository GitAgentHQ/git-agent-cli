package cmd

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphExternalRefsCmd = &cobra.Command{
	Use:   "external-refs",
	Short: "Show call/field sites into external packages",
	Long: `List unresolved references whose qualifier matches an indexed import
alias — i.e. call/field-read sites that target symbols in external packages
(github.com/spf13/pflag, fmt, os, ...). The AST index only parses git-tracked
files in this repo, so external-package symbols are never indexed as nodes;
this command surfaces where the codebase reaches into them instead of leaving
those references silently unresolved. Read-only.

Group by external package; each entry shows the referencing symbol, the file
and line, and the qualified reference name. Use this when ` + "`graph callers`" + `
reports a symbol is "exported by external package".`,
	Args: cobra.NoArgs,
	RunE: runGraphExternalRefs,
}

type externalRefEntry struct {
	Package       string `json:"package"`
	ReferenceName string `json:"reference_name"`
	FromSymbol    string `json:"from_symbol"`
	FromFile      string `json:"from_file"`
	FromLine      int    `json:"from_line"`
	Kind          string `json:"kind"`
}

func runGraphExternalRefs(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
	force, _ := cmd.Flags().GetBool("reindex")
	ctx := cmd.Context()

	_, astRepo, client, err := openASTQuery(ctx, "", force, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer client.Close()

	imports, err := astRepo.ListASTNodesByKind(ctx, graph.ASTNodeKindImport)
	if err != nil {
		return fmt.Errorf("list imports: %w", err)
	}
	// alias → import path, keyed by the default alias (last path segment).
	aliasToPath := make(map[string]string, len(imports))
	for _, imp := range imports {
		path := trimQuote(imp.Name)
		if path == "" {
			continue
		}
		aliasToPath[lastPathSegment(path)] = path
	}
	if len(aliasToPath) == 0 {
		return nil
	}

	refs, err := astRepo.ListUnresolvedRefs(ctx)
	if err != nil {
		return fmt.Errorf("list unresolved refs: %w", err)
	}

	// Resolve from_node_id → symbol name for readable output.
	fromCache := map[string]string{}
	resolveFrom := func(id string) string {
		if name, ok := fromCache[id]; ok {
			return name
		}
		nodes, lookupErr := astRepo.GetASTNodeBySymbol(ctx, id)
		if lookupErr == nil && len(nodes) == 1 {
			fromCache[id] = nodes[0].QualifiedName
			return nodes[0].QualifiedName
		}
		fromCache[id] = id
		return id
	}

	var entries []externalRefEntry
	for _, ref := range refs {
		_, qualifier := splitRefName(ref.ReferenceName)
		if qualifier == "" {
			continue
		}
		pkg, ok := aliasToPath[qualifier]
		if !ok {
			continue
		}
		entries = append(entries, externalRefEntry{
			Package:       pkg,
			ReferenceName: ref.ReferenceName,
			FromSymbol:    resolveFrom(ref.FromNodeID),
			FromFile:      ref.FilePath,
			FromLine:      ref.Line,
			Kind:          ref.ReferenceKind,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Package != entries[j].Package {
			return entries[i].Package < entries[j].Package
		}
		if entries[i].FromFile != entries[j].FromFile {
			return entries[i].FromFile < entries[j].FromFile
		}
		return entries[i].FromLine < entries[j].FromLine
	})

	out := cmd.OutOrStdout()
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, map[string]any{
			"results": entries,
			"total":   len(entries),
		})
	}
	renderExternalRefs(out, entries)
	return nil
}

func renderExternalRefs(out io.Writer, entries []externalRefEntry) {
	fmt.Fprintf(out, "External-package references (%d):\n", len(entries))
	if len(entries) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	var prevPkg string
	for _, e := range entries {
		if e.Package != prevPkg {
			fmt.Fprintf(out, "\n%s\n", e.Package)
			prevPkg = e.Package
		}
		fmt.Fprintf(out, "  %s  %s  %s:%d  (%s)\n",
			e.ReferenceName, e.FromSymbol, e.FromFile, e.FromLine, e.Kind)
	}
}

func trimQuote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}

func init() {
	graphExternalRefsCmd.Flags().Bool("reindex", false, "force a full AST re-index before listing")
	graphExternalRefsCmd.Flags().Bool("json", false, "emit the results as JSON")
	graphExternalRefsCmd.Flags().Bool("text", false, "emit the results as text")
	graphExternalRefsCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphExternalRefsCmd)
}
