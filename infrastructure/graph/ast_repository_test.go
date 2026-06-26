package graph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestASTRepository(t *testing.T) {
	ctx := context.Background()

	setupDB := func(t *testing.T) (*SQLiteClient, graph.ASTRepository) {
		t.Helper()
		dbPath := filepath.Join(t.TempDir(), "ast_test.db")
		client := NewSQLiteClient(dbPath)
		if err := client.Open(ctx); err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { client.Close(); os.Remove(dbPath) })
		if err := client.InitSchema(ctx); err != nil {
			t.Fatalf("init schema: %v", err)
		}
		repo := NewSQLiteASTRepository(client)
		return client, repo
	}

	t.Run("upsert and retrieve an AST node by name", func(t *testing.T) {
		_, repo := setupDB(t)
		node := graph.ASTNode{
			ID:            "function:handler.go:HandleRequest",
			Kind:          graph.ASTNodeKindFunction,
			Name:          "HandleRequest",
			QualifiedName: "handler.go::HandleRequest",
			FilePath:      "handler.go",
			Language:      "go",
			StartLine:     3,
			EndLine:       5,
			UpdatedAt:     1000,
		}
		if err := repo.UpsertASTNode(ctx, node); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		found, err := repo.GetASTNodeByName(ctx, "HandleRequest")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(found) == 0 {
			t.Fatal("expected to find HandleRequest")
		}
		if found[0].Kind != graph.ASTNodeKindFunction {
			t.Errorf("expected kind function, got %s", found[0].Kind)
		}
	})

	t.Run("upsert and retrieve an AST node by qualified_name", func(t *testing.T) {
		_, repo := setupDB(t)
		node := graph.ASTNode{
			ID:            "function:handler.go:HandleRequest",
			Kind:          graph.ASTNodeKindFunction,
			Name:          "HandleRequest",
			QualifiedName: "handler.go::HandleRequest",
			FilePath:      "handler.go",
			Language:      "go",
			StartLine:     3,
			EndLine:       5,
			UpdatedAt:     1000,
		}
		if err := repo.UpsertASTNode(ctx, node); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		found, err := repo.GetASTNodeByQualifiedName(ctx, "handler.go::HandleRequest")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if found == nil {
			t.Fatal("expected to find node by qualified_name")
		}
		if found.Name != "HandleRequest" {
			t.Errorf("expected name HandleRequest, got %s", found.Name)
		}
	})

	t.Run("upsert AST edges and query callers", func(t *testing.T) {
		_, repo := setupDB(t)
		runNode := graph.ASTNode{ID: "function:handler.go:run", Kind: graph.ASTNodeKindFunction, Name: "run", QualifiedName: "handler.go::run", FilePath: "handler.go", Language: "go", StartLine: 3, EndLine: 5, UpdatedAt: 1000}
		processNode := graph.ASTNode{ID: "function:handler.go:process", Kind: graph.ASTNodeKindFunction, Name: "process", QualifiedName: "handler.go::process", FilePath: "handler.go", Language: "go", StartLine: 6, EndLine: 7, UpdatedAt: 1000}
		repo.UpsertASTNode(ctx, runNode)
		repo.UpsertASTNode(ctx, processNode)
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: "function:handler.go:run", Target: "function:handler.go:process", Kind: graph.ASTEdgeKindCalls, Line: 4, Provenance: "tree-sitter"})

		callers, err := repo.GetCallers(ctx, "function:handler.go:process", 1)
		if err != nil {
			t.Fatalf("callers: %v", err)
		}
		if len(callers) == 0 {
			t.Fatal("expected callers for process")
		}
		if callers[0].Node.Name != "run" {
			t.Errorf("expected caller run, got %s", callers[0].Node.Name)
		}
	})

	t.Run("upsert AST edges and query callees", func(t *testing.T) {
		_, repo := setupDB(t)
		runNode := graph.ASTNode{ID: "function:handler.go:run", Kind: graph.ASTNodeKindFunction, Name: "run", QualifiedName: "handler.go::run", FilePath: "handler.go", Language: "go", StartLine: 3, EndLine: 5, UpdatedAt: 1000}
		processNode := graph.ASTNode{ID: "function:handler.go:process", Kind: graph.ASTNodeKindFunction, Name: "process", QualifiedName: "handler.go::process", FilePath: "handler.go", Language: "go", StartLine: 6, EndLine: 7, UpdatedAt: 1000}
		repo.UpsertASTNode(ctx, runNode)
		repo.UpsertASTNode(ctx, processNode)
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: "function:handler.go:run", Target: "function:handler.go:process", Kind: graph.ASTEdgeKindCalls, Line: 4, Provenance: "tree-sitter"})

		callees, err := repo.GetCallees(ctx, "function:handler.go:run", 1)
		if err != nil {
			t.Fatalf("callees: %v", err)
		}
		if len(callees) == 0 {
			t.Fatal("expected callees for run")
		}
		if callees[0].Node.Name != "process" {
			t.Errorf("expected callee process, got %s", callees[0].Node.Name)
		}
	})

	t.Run("impact radius via BFS on incoming edges", func(t *testing.T) {
		_, repo := setupDB(t)
		nodes := []graph.ASTNode{
			{ID: "function:a.go:A", Kind: graph.ASTNodeKindFunction, Name: "A", QualifiedName: "a.go::A", FilePath: "a.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000},
			{ID: "function:b.go:B", Kind: graph.ASTNodeKindFunction, Name: "B", QualifiedName: "b.go::B", FilePath: "b.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000},
			{ID: "function:c.go:C", Kind: graph.ASTNodeKindFunction, Name: "C", QualifiedName: "c.go::C", FilePath: "c.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000},
			{ID: "function:d.go:D", Kind: graph.ASTNodeKindFunction, Name: "D", QualifiedName: "d.go::D", FilePath: "d.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000},
		}
		for _, n := range nodes {
			repo.UpsertASTNode(ctx, n)
		}
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: "function:a.go:A", Target: "function:b.go:B", Kind: graph.ASTEdgeKindCalls, Provenance: "tree-sitter"})
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: "function:b.go:B", Target: "function:c.go:C", Kind: graph.ASTEdgeKindCalls, Provenance: "tree-sitter"})
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: "function:c.go:C", Target: "function:d.go:D", Kind: graph.ASTEdgeKindCalls, Provenance: "tree-sitter"})

		result, err := repo.GetImpactRadius(ctx, "function:d.go:D", 3)
		if err != nil {
			t.Fatalf("impact: %v", err)
		}
		if result.TotalFound < 3 {
			t.Fatalf("expected at least 3 impacted nodes, got %d", result.TotalFound)
		}

		foundC := false
		foundB := false
		foundA := false
		for _, entry := range result.Impacted {
			switch entry.Node.Name {
			case "C":
				foundC = true
				if entry.Depth != 1 {
					t.Errorf("expected C depth 1, got %d", entry.Depth)
				}
				if entry.Edge.Source != "function:c.go:C" || entry.Edge.Target != "function:d.go:D" {
					t.Errorf("expected C edge C->D, got %s->%s", entry.Edge.Source, entry.Edge.Target)
				}
			case "B":
				foundB = true
				if entry.Depth != 2 {
					t.Errorf("expected B depth 2, got %d", entry.Depth)
				}
				if entry.Edge.Source != "function:b.go:B" || entry.Edge.Target != "function:c.go:C" {
					t.Errorf("expected B edge B->C, got %s->%s", entry.Edge.Source, entry.Edge.Target)
				}
			case "A":
				foundA = true
				if entry.Depth != 3 {
					t.Errorf("expected A depth 3, got %d", entry.Depth)
				}
				if entry.Edge.Source != "function:a.go:A" || entry.Edge.Target != "function:b.go:B" {
					t.Errorf("expected A edge A->B, got %s->%s", entry.Edge.Source, entry.Edge.Target)
				}
			}
		}
		if !foundC || !foundB || !foundA {
			t.Errorf("expected C/B/A in impact, got: %v", impactNames(result))
		}
	})

	t.Run("symbol search finds nodes by name", func(t *testing.T) {
		_, repo := setupDB(t)
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:h.go:HandleRequest", Kind: graph.ASTNodeKindFunction, Name: "HandleRequest", QualifiedName: "h.go::HandleRequest", FilePath: "h.go", Language: "go", StartLine: 3, EndLine: 5, UpdatedAt: 1000})
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:h.go:HandleError", Kind: graph.ASTNodeKindFunction, Name: "HandleError", QualifiedName: "h.go::HandleError", FilePath: "h.go", Language: "go", StartLine: 7, EndLine: 9, UpdatedAt: 1000})

		results, err := repo.SearchASTNodes(ctx, "Handle", nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(results))
		}
		names := make(map[string]bool)
		for _, r := range results {
			names[r.Node.Name] = true
		}
		if !names["HandleRequest"] || !names["HandleError"] {
			t.Errorf("expected HandleRequest and HandleError in search, got %v", names)
		}
	})

	t.Run("upsert unresolved refs", func(t *testing.T) {
		_, repo := setupDB(t)
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:h.go:run", Kind: graph.ASTNodeKindFunction, Name: "run", QualifiedName: "h.go::run", FilePath: "h.go", Language: "go", StartLine: 3, EndLine: 5, UpdatedAt: 1000})
		ref := graph.ASTUnresolvedRef{
			FromNodeID:    "function:h.go:run",
			ReferenceName: "Println",
			ReferenceKind: "calls",
			Line:          4,
			FilePath:      "h.go",
			Language:      "go",
		}
		if err := repo.UpsertUnresolvedRef(ctx, ref); err != nil {
			t.Fatalf("upsert ref: %v", err)
		}
	})

	t.Run("delete AST nodes for a file removes all its symbols", func(t *testing.T) {
		_, repo := setupDB(t)
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "file:handler.go", Kind: graph.ASTNodeKindFile, Name: "handler.go", QualifiedName: "handler.go", FilePath: "handler.go", Language: "go", StartLine: 1, EndLine: 10, UpdatedAt: 1000})
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:handler.go:HandleRequest", Kind: graph.ASTNodeKindFunction, Name: "HandleRequest", QualifiedName: "handler.go::HandleRequest", FilePath: "handler.go", Language: "go", StartLine: 3, EndLine: 5, UpdatedAt: 1000})
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:handler.go:process", Kind: graph.ASTNodeKindFunction, Name: "process", QualifiedName: "handler.go::process", FilePath: "handler.go", Language: "go", StartLine: 6, EndLine: 7, UpdatedAt: 1000})

		if err := repo.DeleteASTNodesForFile(ctx, "handler.go"); err != nil {
			t.Fatalf("delete: %v", err)
		}

		found, err := repo.GetASTNodeByName(ctx, "HandleRequest")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(found) != 0 {
			t.Errorf("expected no results after delete, got %d", len(found))
		}
	})

	t.Run("delete AST nodes except files removes stale nodes and edges", func(t *testing.T) {
		_, repo := setupDB(t)
		keep := graph.ASTNode{ID: "function:keep.go:keep", Kind: graph.ASTNodeKindFunction, Name: "keep", QualifiedName: "keep.go::keep", FilePath: "keep.go", Language: "go"}
		stale := graph.ASTNode{ID: "function:stale.go:stale", Kind: graph.ASTNodeKindFunction, Name: "stale", QualifiedName: "stale.go::stale", FilePath: "stale.go", Language: "go"}
		for _, n := range []graph.ASTNode{keep, stale} {
			if err := repo.UpsertASTNode(ctx, n); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: stale.ID, Target: keep.ID, Kind: graph.ASTEdgeKindCalls}); err != nil {
			t.Fatal(err)
		}

		if err := repo.DeleteASTNodesExceptFiles(ctx, []string{"keep.go"}); err != nil {
			t.Fatalf("delete except files: %v", err)
		}
		found, err := repo.GetASTNodeByName(ctx, "stale")
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 0 {
			t.Fatalf("expected stale node deleted, got %+v", found)
		}
		callers, err := repo.GetCallers(ctx, keep.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(callers) != 0 {
			t.Fatalf("expected stale edge deleted, got %+v", callers)
		}
	})

	t.Run("AST transaction rolls back on error", func(t *testing.T) {
		_, repo := setupDB(t)
		sqliteRepo := repo.(*SQLiteASTRepository)
		sentinel := errors.New("boom")
		err := sqliteRepo.RunInTx(ctx, func() error {
			if err := sqliteRepo.UpsertASTNode(ctx, graph.ASTNode{
				ID:            "function:tx.go:temp",
				Kind:          graph.ASTNodeKindFunction,
				Name:          "temp",
				QualifiedName: "tx.go::temp",
				FilePath:      "tx.go",
				Language:      "go",
			}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
		found, err := sqliteRepo.GetASTNodeByName(ctx, "temp")
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 0 {
			t.Fatalf("expected transaction rollback to remove temp node, got %+v", found)
		}
	})

	t.Run("impact radius expands container members", func(t *testing.T) {
		_, repo := setupDB(t)
		// Service struct contains method Save; caller calls Save directly.
		svc := graph.ASTNode{ID: "struct:s.go:Service", Kind: graph.ASTNodeKindStruct, Name: "Service", QualifiedName: "s.go::Service", FilePath: "s.go", Language: "go", StartLine: 1, EndLine: 10, UpdatedAt: 1000}
		save := graph.ASTNode{ID: "method:s.go:Service.Save", Kind: graph.ASTNodeKindMethod, Name: "Save", QualifiedName: "s.go::Service.Save", FilePath: "s.go", Language: "go", StartLine: 2, EndLine: 4, UpdatedAt: 1000}
		caller := graph.ASTNode{ID: "function:h.go:handler", Kind: graph.ASTNodeKindFunction, Name: "handler", QualifiedName: "h.go::handler", FilePath: "h.go", Language: "go", StartLine: 1, EndLine: 3, UpdatedAt: 1000}
		for _, n := range []graph.ASTNode{svc, save, caller} {
			repo.UpsertASTNode(ctx, n)
		}
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: svc.ID, Target: save.ID, Kind: graph.ASTEdgeKindContains, Provenance: "tree-sitter"})
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: caller.ID, Target: save.ID, Kind: graph.ASTEdgeKindCalls, Provenance: "tree-sitter"})

		// Impact on the Service struct should surface handler (caller of Save)
		// via container expansion, even though no edge points directly at Service.
		result, err := repo.GetImpactRadius(ctx, svc.ID, 1)
		if err != nil {
			t.Fatalf("impact: %v", err)
		}
		foundHandler := false
		for _, e := range result.Impacted {
			if e.Node.Name == "handler" {
				foundHandler = true
			}
		}
		if !foundHandler {
			names := impactNames(result)
			t.Errorf("expected container expansion to surface handler, got %v", names)
		}
	})

	t.Run("edge metadata persists and is returned", func(t *testing.T) {
		_, repo := setupDB(t)
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:a.go:A", Kind: graph.ASTNodeKindFunction, Name: "A", QualifiedName: "a.go::A", FilePath: "a.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000})
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:b.go:B", Kind: graph.ASTNodeKindFunction, Name: "B", QualifiedName: "b.go::B", FilePath: "b.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000})
		meta := `{"resolvedBy":"resolver","confidence":0.9}`
		if err := repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: "function:a.go:A", Target: "function:b.go:B", Kind: graph.ASTEdgeKindCalls, Provenance: "resolver", Metadata: meta}); err != nil {
			t.Fatalf("upsert edge: %v", err)
		}
		callees, err := repo.GetCallees(ctx, "function:a.go:A", 1)
		if err != nil {
			t.Fatalf("callees: %v", err)
		}
		if len(callees) != 1 || callees[0].Edge.Metadata != meta {
			t.Errorf("expected metadata %q, got %+v", meta, callees)
		}
	})

	t.Run("FTS5 search ranks exact name match above substring", func(t *testing.T) {
		_, repo := setupDB(t)
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:h.go:Handle", Kind: graph.ASTNodeKindFunction, Name: "Handle", QualifiedName: "h.go::Handle", FilePath: "h.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000})
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:h.go:HandleRequest", Kind: graph.ASTNodeKindFunction, Name: "HandleRequest", QualifiedName: "h.go::HandleRequest", FilePath: "h.go", Language: "go", StartLine: 3, EndLine: 5, UpdatedAt: 1000})

		results, err := repo.SearchASTNodes(ctx, "Handle", nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(results))
		}
		// The exact "Handle" match should outrank the longer "HandleRequest".
		if results[0].Node.Name != "Handle" {
			t.Errorf("expected exact match Handle ranked first, got %s (score=%v)", results[0].Node.Name, results[0].Score)
		}
	})

	t.Run("FTS5 search handles punctuation without syntax error", func(t *testing.T) {
		_, repo := setupDB(t)
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:h.go:Run", Kind: graph.ASTNodeKindFunction, Name: "Run", QualifiedName: "h.go::Run", FilePath: "h.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000})
		// A query with FTS5 metacharacters must be sanitized, not crash.
		results, err := repo.SearchASTNodes(ctx, "Run();", nil)
		if err != nil {
			t.Fatalf("search with punctuation: %v", err)
		}
		if len(results) != 1 || results[0].Node.Name != "Run" {
			t.Errorf("expected Run, got %+v", results)
		}
	})

	t.Run("LIKE fallback with kind filter does not error", func(t *testing.T) {
		_, repo := setupDB(t)
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "function:h.go:Run", Kind: graph.ASTNodeKindFunction, Name: "Run", QualifiedName: "h.go::Run", FilePath: "h.go", Language: "go", StartLine: 1, EndLine: 2, UpdatedAt: 1000})
		repo.UpsertASTNode(ctx, graph.ASTNode{ID: "struct:h.go:Thing", Kind: graph.ASTNodeKindStruct, Name: "Thing", QualifiedName: "h.go::Thing", FilePath: "h.go", Language: "go", StartLine: 3, EndLine: 4, UpdatedAt: 1000})
		// An empty/all-punctuation query sanitizes to "" and takes the LIKE
		// branch; a non-empty kind filter previously failed with
		// "no such column: n.kind" because that branch had no table alias.
		results, err := repo.SearchASTNodes(ctx, "", []graph.ASTNodeKind{graph.ASTNodeKindFunction})
		if err != nil {
			t.Fatalf("LIKE fallback with kind filter: %v", err)
		}
		if len(results) != 1 || results[0].Node.Name != "Run" {
			t.Errorf("expected only the Run function, got %+v", results)
		}
	})

	t.Run("impact reaches promoted method via struct embedding", func(t *testing.T) {
		_, repo := setupDB(t)
		// Service embeds Base; Base has method Save; handler calls Save directly.
		svc := graph.ASTNode{ID: "struct:s.go:Service", Kind: graph.ASTNodeKindStruct, Name: "Service", QualifiedName: "s.go::Service", FilePath: "s.go", Language: "go", StartLine: 1, EndLine: 3, UpdatedAt: 1000}
		base := graph.ASTNode{ID: "struct:s.go:Base", Kind: graph.ASTNodeKindStruct, Name: "Base", QualifiedName: "s.go::Base", FilePath: "s.go", Language: "go", StartLine: 1, EndLine: 1, UpdatedAt: 1000}
		save := graph.ASTNode{ID: "method:s.go:Base.Save", Kind: graph.ASTNodeKindMethod, Name: "Save", QualifiedName: "s.go::Base.Save", FilePath: "s.go", Language: "go", StartLine: 2, EndLine: 2, UpdatedAt: 1000}
		caller := graph.ASTNode{ID: "function:h.go:handler", Kind: graph.ASTNodeKindFunction, Name: "handler", QualifiedName: "h.go::handler", FilePath: "h.go", Language: "go", StartLine: 1, EndLine: 3, UpdatedAt: 1000}
		for _, n := range []graph.ASTNode{svc, base, save, caller} {
			repo.UpsertASTNode(ctx, n)
		}
		// The embedding: Service extends Base. Base contains Save.
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: svc.ID, Target: base.ID, Kind: graph.ASTEdgeKindExtends, Provenance: "tree-sitter"})
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: base.ID, Target: save.ID, Kind: graph.ASTEdgeKindContains, Provenance: "tree-sitter"})
		repo.UpsertASTEdge(ctx, graph.ASTEdge{Source: caller.ID, Target: save.ID, Kind: graph.ASTEdgeKindCalls, Provenance: "tree-sitter"})

		// Impact on Service: container expansion surfaces Base's members, and
		// the extends edge lets the BFS reach Save and its caller (handler).
		result, err := repo.GetImpactRadius(ctx, svc.ID, 3)
		if err != nil {
			t.Fatalf("impact: %v", err)
		}
		found := map[string]bool{}
		for _, e := range result.Impacted {
			found[e.Node.Name] = true
		}
		if !found["Save"] {
			t.Errorf("expected impact to reach promoted method Save via embedding, got %v", impactNames(result))
		}
		if !found["handler"] {
			t.Errorf("expected impact to reach handler (caller of promoted Save), got %v", impactNames(result))
		}
	})
}

func impactNames(r *graph.ASTImpactResult) []string {
	out := make([]string, len(r.Impacted))
	for i, e := range r.Impacted {
		out[i] = e.Node.Name + "(d" + strconv.Itoa(e.Depth) + ")"
	}
	return out
}
