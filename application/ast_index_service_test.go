//go:build cgo

package application

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	infraExtraction "github.com/gitagenthq/git-agent/infrastructure/extraction"
	graphinfra "github.com/gitagenthq/git-agent/infrastructure/graph"
)

func TestASTIndexService_IndexFileRunsResolver(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), `package main

func run() string {
	return helper()
}
`)

	repo, cleanup := setupIndexTestRepo(t)
	defer cleanup()
	helper := graph.ASTNode{
		ID:            "function:b.go:helper",
		Kind:          graph.ASTNodeKindFunction,
		Name:          "helper",
		QualifiedName: "b.go::helper",
		FilePath:      "b.go",
		Language:      "go",
	}
	if err := repo.UpsertASTNode(ctx, helper); err != nil {
		t.Fatal(err)
	}

	extractor := infraExtraction.NewTreeSitterExtractor("go", infraExtraction.GoExtractor())
	svc := NewASTIndexService(repo, fakeTrackedFiles{}, extractor)
	result, err := svc.IndexFile(ctx, root, "a.go")
	if err != nil {
		t.Fatalf("index file: %v", err)
	}
	if result.ResolvedRefs != 1 {
		t.Fatalf("expected IndexFile to resolve 1 ref, got %+v", result)
	}

	callers, err := repo.GetCallers(ctx, helper.ID, 1)
	if err != nil {
		t.Fatalf("get callers: %v", err)
	}
	if len(callers) != 1 || callers[0].Node.Name != "run" {
		t.Fatalf("expected run caller after IndexFile resolver pass, got %+v", callers)
	}
}

func TestASTIndexService_IndexAllPropagatesReadErrors(t *testing.T) {
	ctx := context.Background()
	repo, cleanup := setupIndexTestRepo(t)
	defer cleanup()

	extractor := infraExtraction.NewTreeSitterExtractor("go", infraExtraction.GoExtractor())
	svc := NewASTIndexService(repo, fakeTrackedFiles{files: []string{"missing.go"}}, extractor)
	if _, err := svc.IndexAll(ctx, t.TempDir()); err == nil {
		t.Fatal("expected missing tracked file to return an error")
	}
}

func TestASTIndexService_IndexAllDeletesStaleFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), `package main

func main() {}
`)
	repo, cleanup := setupIndexTestRepo(t)
	defer cleanup()
	stale := graph.ASTNode{
		ID:            "function:stale.go:old",
		Kind:          graph.ASTNodeKindFunction,
		Name:          "old",
		QualifiedName: "stale.go::old",
		FilePath:      "stale.go",
		Language:      "go",
	}
	if err := repo.UpsertASTNode(ctx, stale); err != nil {
		t.Fatal(err)
	}

	extractor := infraExtraction.NewTreeSitterExtractor("go", infraExtraction.GoExtractor())
	svc := NewASTIndexService(repo, fakeTrackedFiles{files: []string{"main.go"}}, extractor)
	if _, err := svc.IndexAll(ctx, root); err != nil {
		t.Fatalf("index all: %v", err)
	}

	found, err := repo.GetASTNodeByName(ctx, "old")
	if err != nil {
		t.Fatalf("lookup stale node: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected stale.go nodes to be deleted, got %+v", found)
	}
}

type fakeTrackedFiles struct {
	files []string
	err   error
}

func (f fakeTrackedFiles) TrackedFiles(ctx context.Context, pathspec string) ([]string, error) {
	return f.files, f.err
}

func setupIndexTestRepo(t *testing.T) (*graphinfra.SQLiteASTRepository, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ast.db")
	client := graphinfra.NewSQLiteClient(dbPath)
	ctx := context.Background()
	if err := client.Open(ctx); err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return graphinfra.NewSQLiteASTRepository(client), func() { _ = client.Close() }
}

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
