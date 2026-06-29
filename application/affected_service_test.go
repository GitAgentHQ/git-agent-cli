package application

import (
	"context"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestAffected_ChangedTestFileReturnsItsOwnTests(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	greet := graph.ASTNode{ID: "greet", Kind: graph.ASTNodeKindFunction, Name: "greet", QualifiedName: "main.go::greet", FilePath: "main.go", Language: "go", StartLine: 3}
	testGreet := graph.ASTNode{ID: "testgreet", Kind: graph.ASTNodeKindFunction, Name: "TestGreet", QualifiedName: "main_test.go::TestGreet", FilePath: "main_test.go", Language: "go", StartLine: 5}
	for _, n := range []graph.ASTNode{greet, testGreet} {
		if err := repo.UpsertASTNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	// TestGreet calls greet.
	if err := repo.UpsertUnresolvedRef(ctx, graph.ASTUnresolvedRef{
		FromNodeID: "testgreet", ReferenceName: "greet", ReferenceKind: string(graph.ASTEdgeKindCalls), Line: 6, FilePath: "main_test.go", Language: "go",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReferenceResolver(repo, nil).Resolve(ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	svc := NewAffectedService(repo)

	// Changing the test file itself must surface its own test function (depth 0):
	// a Go test func has no callers, so the transitive walk never reaches it.
	res, err := svc.Affected(ctx, []string{"main_test.go"}, 2)
	if err != nil {
		t.Fatalf("affected: %v", err)
	}
	if res.Total != 1 || len(res.Tests) != 1 {
		t.Fatalf("changed test file: total = %d, tests = %+v, want 1", res.Total, res.Tests)
	}
	if got := res.Tests[0]; got.Symbol != "TestGreet" || got.TestFile != "main_test.go" || got.Depth != 0 {
		t.Errorf("entry = %+v, want TestGreet in main_test.go at depth 0", got)
	}

	// Changing the production file still surfaces the test transitively (depth 1).
	res, err = svc.Affected(ctx, []string{"main.go"}, 2)
	if err != nil {
		t.Fatalf("affected: %v", err)
	}
	if res.Total != 1 || res.Tests[0].Symbol != "TestGreet" || res.Tests[0].Depth != 1 {
		t.Fatalf("changed prod file: tests = %+v, want TestGreet at depth 1", res.Tests)
	}

	// Changing both must list the test once, preferring the depth-0 attribution.
	res, err = svc.Affected(ctx, []string{"main.go", "main_test.go"}, 2)
	if err != nil {
		t.Fatalf("affected: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("changed both: total = %d, tests = %+v, want 1 (deduped)", res.Total, res.Tests)
	}
	if res.Tests[0].Depth != 0 {
		t.Errorf("deduped entry depth = %d, want 0 (you-changed-this-test wins)", res.Tests[0].Depth)
	}
}

func TestAffected_NonTestSymbolsInChangedTestFileExcluded(t *testing.T) {
	repo := setupASTRepo(t)
	ctx := context.Background()

	// A helper (not a Test/Benchmark/Example func) in a test file is not a unit to run.
	helper := graph.ASTNode{ID: "h", Kind: graph.ASTNodeKindFunction, Name: "newFixture", QualifiedName: "x_test.go::newFixture", FilePath: "x_test.go", Language: "go", StartLine: 4}
	if err := repo.UpsertASTNode(ctx, helper); err != nil {
		t.Fatal(err)
	}

	res, err := NewAffectedService(repo).Affected(ctx, []string{"x_test.go"}, 2)
	if err != nil {
		t.Fatalf("affected: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("total = %d, want 0 (a non-test helper is not a runnable test)", res.Total)
	}
}
