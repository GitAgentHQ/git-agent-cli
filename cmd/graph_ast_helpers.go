package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gitagenthq/git-agent/domain/graph"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// openASTQuery opens the graph db, ensures the AST index is fresh, and returns
// the repo root, AST repo, and db client (caller closes it). When symbol is
// non-empty the index is ensured for that symbol; otherwise the whole index is
// ensured (for graph query / node by name). root is returned so commands that
// need to read working-tree files (graph node) can resolve repo-relative paths.
func openASTQuery(ctx context.Context, symbol string, force bool, progress io.Writer) (string, graph.ASTRepository, *infraGraph.SQLiteClient, error) {
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return "", nil, nil, fmt.Errorf("repo root: %w", err)
	}
	_, client, err := openGraphDB(ctx, root)
	if err != nil {
		return "", nil, nil, err
	}
	astRepo := infraGraph.NewSQLiteASTRepository(client)
	stateRepo := infraGraph.NewSQLiteRepository(client)
	graphGit := infraGit.NewGraphClient(root)
	if err := ensureASTIndex(ctx, root, astRepo, stateRepo, graphGit, symbol, force, progress); err != nil {
		client.Close()
		return "", nil, nil, err
	}
	return root, astRepo, client, nil
}

// symbolNotFoundHint returns a "symbol not found" error. When the symbol looks
// like an external-package reference (qualifier matches an import alias in the
// indexed set), the error points the user at `graph external-refs` instead of
// the generic message — external packages are not indexed by design.
func symbolNotFoundHint(ctx context.Context, astRepo graph.ASTRepository, symbol string, _ io.Writer) error {
	if pkg := externalPackageFor(ctx, astRepo, symbol); pkg != "" {
		return fmt.Errorf("symbol %q is exported by external package %q, which is not indexed; "+
			"run `git-agent graph external-refs` to list call sites into it", symbol, pkg)
	}
	return fmt.Errorf("symbol %q not found", symbol)
}

// externalPackageFor returns the import path of an external package whose
// alias matches the qualifier of symbol (e.g. symbol "pflag.Lookup" →
// "github.com/spf13/pflag"), or "" when the symbol is not an external-package
// reference.
//
// Two cases:
//   - symbol has a qualifier ("pflag.Lookup"): match the qualifier against the
//     last segment of any indexed import path.
//   - symbol is a bare name ("Lookup"): consult unresolved refs whose trailing
//     name matches and whose qualifier is an import alias; return that alias's
//     import path. This catches the common case where the user asks for a
//     bare external symbol name.
func externalPackageFor(ctx context.Context, astRepo graph.ASTRepository, symbol string) string {
	lookupName, qualifier := splitRefName(symbol)

	imports, err := astRepo.ListASTNodesByKind(ctx, graph.ASTNodeKindImport)
	if err != nil {
		return ""
	}
	// importAlias → import path, keyed by the default alias (last path segment).
	aliasToPath := make(map[string]string, len(imports))
	for _, imp := range imports {
		path := strings.Trim(imp.Name, "\"")
		if path == "" {
			continue
		}
		aliasToPath[lastPathSegment(path)] = path
	}

	if qualifier != "" {
		if path, ok := aliasToPath[qualifier]; ok {
			return path
		}
		return ""
	}

	// Bare name: look for an unresolved ref whose trailing name matches and
	// whose qualifier is a known import alias.
	if lookupName == "" {
		return ""
	}
	refs, err := astRepo.ListUnresolvedRefsMatching(ctx, nil, []string{lookupName})
	if err != nil {
		return ""
	}
	for _, ref := range refs {
		_, q := splitRefName(ref.ReferenceName)
		if q == "" {
			continue
		}
		if path, ok := aliasToPath[q]; ok {
			return path
		}
	}
	return ""
}

func splitRefName(name string) (lookup, qualifier string) {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[dot+1:], name[:dot]
	}
	return name, ""
}

func lastPathSegment(path string) string {
	// strip a trailing quote/alias if present
	path = strings.Trim(path, "\"")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
