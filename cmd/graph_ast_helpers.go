package cmd

import (
	"context"
	"fmt"
	"io"

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
