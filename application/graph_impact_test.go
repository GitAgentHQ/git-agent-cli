package application

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	graphinfra "github.com/gitagenthq/git-agent/infrastructure/graph"
)

func setupImpactTest(t *testing.T) *graphinfra.SQLiteRepository {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := graphinfra.NewSQLiteClient(dbPath)
	repo := graphinfra.NewSQLiteRepository(client)
	ctx := context.Background()
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func seedCoChanged(t *testing.T, repo *graphinfra.SQLiteRepository, fileA, fileB string, count int, strength float64) {
	t.Helper()
	// Ensure canonical ordering: file_a < file_b.
	if fileA > fileB {
		fileA, fileB = fileB, fileA
	}
	_, err := repo.Client().DB().Exec(
		`INSERT OR REPLACE INTO co_changed (file_a, file_b, coupling_count, coupling_strength, last_coupled_hash)
		 VALUES (?, ?, ?, ?, ?)`,
		fileA, fileB, count, strength, "abc123",
	)
	if err != nil {
		t.Fatalf("seed co_changed (%s, %s): %v", fileA, fileB, err)
	}
}

func seedRename(t *testing.T, repo *graphinfra.SQLiteRepository, oldPath, newPath string) {
	t.Helper()
	ctx := context.Background()
	if err := repo.CreateRename(ctx, oldPath, newPath, "rename_commit"); err != nil {
		t.Fatalf("seed rename (%s -> %s): %v", oldPath, newPath, err)
	}
}

func TestImpactService_BasicCoChange(t *testing.T) {
	repo := setupImpactTest(t)

	seedCoChanged(t, repo, "main.go", "util.go", 5, 0.8)
	seedCoChanged(t, repo, "main.go", "config.go", 4, 0.6)
	seedCoChanged(t, repo, "main.go", "readme.md", 3, 0.3)

	svc := NewImpactService(repo)
	result, err := svc.Impact(context.Background(), graph.ImpactRequest{
		Path: "main.go",
	})
	if err != nil {
		t.Fatalf("Impact() error = %v", err)
	}

	if result.Target != "main.go" {
		t.Errorf("Target = %q, want %q", result.Target, "main.go")
	}
	if len(result.CoChanged) != 3 {
		t.Fatalf("len(CoChanged) = %d, want 3", len(result.CoChanged))
	}

	// Verify sorted by strength descending.
	if result.CoChanged[0].Path != "util.go" {
		t.Errorf("CoChanged[0].Path = %q, want %q", result.CoChanged[0].Path, "util.go")
	}
	if result.CoChanged[0].CouplingStrength != 0.8 {
		t.Errorf("CoChanged[0].CouplingStrength = %f, want 0.8", result.CoChanged[0].CouplingStrength)
	}
	if result.CoChanged[1].Path != "config.go" {
		t.Errorf("CoChanged[1].Path = %q, want %q", result.CoChanged[1].Path, "config.go")
	}
	if result.CoChanged[2].Path != "readme.md" {
		t.Errorf("CoChanged[2].Path = %q, want %q", result.CoChanged[2].Path, "readme.md")
	}
}

func TestImpactService_WithRenames(t *testing.T) {
	repo := setupImpactTest(t)

	// old_name.go was renamed to new_name.go.
	seedRename(t, repo, "old_name.go", "new_name.go")

	// Co-change data exists under the old path.
	seedCoChanged(t, repo, "old_name.go", "helper.go", 5, 0.7)
	// Co-change data also exists under the new path.
	seedCoChanged(t, repo, "new_name.go", "config.go", 4, 0.5)

	svc := NewImpactService(repo)
	result, err := svc.Impact(context.Background(), graph.ImpactRequest{
		Path: "new_name.go",
	})
	if err != nil {
		t.Fatalf("Impact() error = %v", err)
	}

	// Should include both helper.go (from old path) and config.go (from new path).
	if len(result.CoChanged) != 2 {
		t.Fatalf("len(CoChanged) = %d, want 2", len(result.CoChanged))
	}

	paths := map[string]bool{}
	for _, e := range result.CoChanged {
		paths[e.Path] = true
	}
	if !paths["helper.go"] {
		t.Error("expected helper.go in results (from old path's co-change history)")
	}
	if !paths["config.go"] {
		t.Error("expected config.go in results (from new path's co-change history)")
	}
}

func TestImpactService_Depth2(t *testing.T) {
	repo := setupImpactTest(t)

	// A <-> B (strength 0.8), B <-> C (strength 0.6)
	seedCoChanged(t, repo, "a.go", "b.go", 5, 0.8)
	seedCoChanged(t, repo, "b.go", "c.go", 4, 0.6)

	svc := NewImpactService(repo)
	result, err := svc.Impact(context.Background(), graph.ImpactRequest{
		Path:  "a.go",
		Depth: 2,
	})
	if err != nil {
		t.Fatalf("Impact() error = %v", err)
	}

	if len(result.CoChanged) != 2 {
		t.Fatalf("len(CoChanged) = %d, want 2", len(result.CoChanged))
	}

	// b.go at depth 1, c.go at depth 2.
	depthMap := map[string]int{}
	for _, e := range result.CoChanged {
		depthMap[e.Path] = e.Depth
	}
	if depthMap["b.go"] != 1 {
		t.Errorf("b.go depth = %d, want 1", depthMap["b.go"])
	}
	if depthMap["c.go"] != 2 {
		t.Errorf("c.go depth = %d, want 2", depthMap["c.go"])
	}
}

func TestImpactService_TopLimit(t *testing.T) {
	repo := setupImpactTest(t)

	// Seed 10 co-change pairs for target.go.
	for i := 0; i < 10; i++ {
		neighbor := "file_" + string(rune('a'+i)) + ".go"
		strength := 0.9 - float64(i)*0.05
		seedCoChanged(t, repo, neighbor, "target.go", 5, strength)
	}

	svc := NewImpactService(repo)
	result, err := svc.Impact(context.Background(), graph.ImpactRequest{
		Path: "target.go",
		Top:  3,
	})
	if err != nil {
		t.Fatalf("Impact() error = %v", err)
	}

	if len(result.CoChanged) != 3 {
		t.Fatalf("len(CoChanged) = %d, want 3", len(result.CoChanged))
	}
	if result.TotalFound != 10 {
		t.Errorf("TotalFound = %d, want 10", result.TotalFound)
	}

	// Verify the top 3 by strength.
	if result.CoChanged[0].CouplingStrength != 0.9 {
		t.Errorf("CoChanged[0].CouplingStrength = %f, want 0.9", result.CoChanged[0].CouplingStrength)
	}
}

func TestImpactService_MinCount(t *testing.T) {
	repo := setupImpactTest(t)

	// Pair with coupling_count=2, below the default MinCount=3.
	seedCoChanged(t, repo, "low.go", "target.go", 2, 0.5)
	// Pair with coupling_count=5, above MinCount=3.
	seedCoChanged(t, repo, "high.go", "target.go", 5, 0.8)

	svc := NewImpactService(repo)
	result, err := svc.Impact(context.Background(), graph.ImpactRequest{
		Path:     "target.go",
		MinCount: 3,
	})
	if err != nil {
		t.Fatalf("Impact() error = %v", err)
	}

	if len(result.CoChanged) != 1 {
		t.Fatalf("len(CoChanged) = %d, want 1", len(result.CoChanged))
	}
	if result.CoChanged[0].Path != "high.go" {
		t.Errorf("CoChanged[0].Path = %q, want %q", result.CoChanged[0].Path, "high.go")
	}
}

func TestImpactService_NonExistentFile(t *testing.T) {
	repo := setupImpactTest(t)

	svc := NewImpactService(repo)
	result, err := svc.Impact(context.Background(), graph.ImpactRequest{
		Path: "does_not_exist.go",
	})
	if err != nil {
		t.Fatalf("Impact() error = %v, want nil", err)
	}
	if len(result.CoChanged) != 0 {
		t.Errorf("len(CoChanged) = %d, want 0", len(result.CoChanged))
	}
	if result.TotalFound != 0 {
		t.Errorf("TotalFound = %d, want 0", result.TotalFound)
	}
}

func TestImpactService_EmptyPath(t *testing.T) {
	repo := setupImpactTest(t)

	svc := NewImpactService(repo)
	_, err := svc.Impact(context.Background(), graph.ImpactRequest{
		Path: "",
	})
	if err == nil {
		t.Fatal("Impact() with empty path should return error")
	}
}
