package application

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	gitinfra "github.com/gitagenthq/git-agent/infrastructure/git"
)

func TestCoChange_BasicPairs(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create 3 commits where a.go and b.go always change together.
	for i := 0; i < 3; i++ {
		writeFile(t, repoDir, "a.go", repeated("package main\n", i+1))
		writeFile(t, repoDir, "b.go", repeated("package main\n", i+1))
		git("add", ".")
		git("commit", "-m", "pair commit")
	}

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDBAt(t, dbPath)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	// Verify co_changed row exists with coupling_count=3.
	db := repo.Client().DB()
	var fileA, fileB string
	var count int
	err = db.QueryRowContext(ctx,
		"SELECT file_a, file_b, coupling_count FROM co_changed WHERE file_a = ? AND file_b = ?",
		"a.go", "b.go",
	).Scan(&fileA, &fileB, &count)
	if err != nil {
		t.Fatalf("query co_changed: %v", err)
	}
	if count != 3 {
		t.Errorf("coupling_count = %d, want 3", count)
	}
}

func TestCoChange_CouplingStrength(t *testing.T) {
	repoDir, git := testRepo(t)

	// a.go changes 10 times, b.go changes 5 times, they co-change 4 times.
	// First, 4 commits where both change.
	for i := 0; i < 4; i++ {
		writeFile(t, repoDir, "a.go", repeated("a", i+1))
		writeFile(t, repoDir, "b.go", repeated("b", i+1))
		git("add", ".")
		git("commit", "-m", "both")
	}
	// Then 6 commits where only a.go changes.
	for i := 0; i < 6; i++ {
		writeFile(t, repoDir, "a.go", repeated("a-solo", i+1))
		git("add", ".")
		git("commit", "-m", "a only")
	}
	// Then 1 commit where only b.go changes.
	writeFile(t, repoDir, "b.go", repeated("b-solo", 1))
	git("add", ".")
	git("commit", "-m", "b only")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDBAt(t, dbPath)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	db := repo.Client().DB()
	var strength float64
	err = db.QueryRowContext(ctx,
		"SELECT coupling_strength FROM co_changed WHERE file_a = ? AND file_b = ?",
		"a.go", "b.go",
	).Scan(&strength)
	if err != nil {
		t.Fatalf("query co_changed: %v", err)
	}

	// a.go changed 10 times, b.go 5 times, co-change 4 times.
	// strength = 4 / max(10, 5) = 0.4
	expected := 0.4
	if math.Abs(strength-expected) > 0.01 {
		t.Errorf("coupling_strength = %f, want %f", strength, expected)
	}
}

func TestCoChange_MinCountFilter(t *testing.T) {
	repoDir, git := testRepo(t)

	// 2 commits where a.go and b.go change together. With minCount=3, no row.
	for i := 0; i < 2; i++ {
		writeFile(t, repoDir, "a.go", repeated("a", i+1))
		writeFile(t, repoDir, "b.go", repeated("b", i+1))
		git("add", ".")
		git("commit", "-m", "pair")
	}

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDBAt(t, dbPath)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	// FullIndex uses minCount=3 by default.
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	db := repo.Client().DB()
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM co_changed").Scan(&count)
	if err != nil {
		t.Fatalf("count co_changed: %v", err)
	}
	if count != 0 {
		t.Errorf("co_changed count = %d, want 0 (minCount=3 filter)", count)
	}
}

func TestCoChange_CanonicalOrdering(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create commits where z.go and a.go change together.
	// The co_changed row should have file_a="a.go" and file_b="z.go".
	for i := 0; i < 3; i++ {
		writeFile(t, repoDir, "z.go", repeated("z", i+1))
		writeFile(t, repoDir, "a.go", repeated("a", i+1))
		git("add", ".")
		git("commit", "-m", "pair")
	}

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDBAt(t, dbPath)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	db := repo.Client().DB()
	var fileA, fileB string
	err = db.QueryRowContext(ctx,
		"SELECT file_a, file_b FROM co_changed LIMIT 1",
	).Scan(&fileA, &fileB)
	if err != nil {
		t.Fatalf("query co_changed: %v", err)
	}
	if fileA != "a.go" || fileB != "z.go" {
		t.Errorf("canonical ordering: got (%q, %q), want (\"a.go\", \"z.go\")", fileA, fileB)
	}
}

func TestCoChange_SkipsLargeCommits(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create 3 small commits where a.go and b.go change together.
	for i := 0; i < 3; i++ {
		writeFile(t, repoDir, "a.go", repeated("a", i+1))
		writeFile(t, repoDir, "b.go", repeated("b", i+1))
		git("add", ".")
		git("commit", "-m", "small pair")
	}

	// Create a large commit with >5 files where a.go and c.go change.
	// We use maxFilesPerCommit=5 for testing.
	writeFile(t, repoDir, "a.go", repeated("a-big", 1))
	writeFile(t, repoDir, "c.go", "c\n")
	writeFile(t, repoDir, "d.go", "d\n")
	writeFile(t, repoDir, "e.go", "e\n")
	writeFile(t, repoDir, "f.go", "f\n")
	writeFile(t, repoDir, "g.go", "g\n")
	git("add", ".")
	git("commit", "-m", "large commit")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDBAt(t, dbPath)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	// Use MaxFilesPerCommit=5 to make the large commit be skipped in co-change.
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{MaxFilesPerCommit: 5})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	db := repo.Client().DB()

	// a.go + b.go should be co-changed (from the 3 small commits).
	var abCount int
	err = db.QueryRowContext(ctx,
		"SELECT coupling_count FROM co_changed WHERE file_a = ? AND file_b = ?",
		"a.go", "b.go",
	).Scan(&abCount)
	if err != nil {
		t.Fatalf("query a.go/b.go co_changed: %v", err)
	}
	if abCount != 3 {
		t.Errorf("a.go/b.go coupling_count = %d, want 3", abCount)
	}

	// a.go + c.go should NOT be co-changed (they only co-occur in the large commit).
	var acCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM co_changed WHERE file_a = ? AND file_b = ?",
		"a.go", "c.go",
	).Scan(&acCount)
	if err != nil {
		t.Fatalf("query a.go/c.go: %v", err)
	}
	if acCount != 0 {
		t.Errorf("a.go/c.go co_changed count = %d, want 0 (large commit excluded)", acCount)
	}
}

func TestCoChange_Incremental(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create 3 commits where a.go and b.go change together.
	for i := 0; i < 3; i++ {
		writeFile(t, repoDir, "a.go", repeated("a", i+1))
		writeFile(t, repoDir, "b.go", repeated("b", i+1))
		git("add", ".")
		git("commit", "-m", "pair commit")
	}

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDBAt(t, dbPath)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)
	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)

	ctx := context.Background()

	// Full index first.
	result1, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("EnsureIndex() error = %v", err)
	}
	if result1.IndexedCommits != 3 {
		t.Fatalf("initial IndexedCommits = %d, want 3", result1.IndexedCommits)
	}

	// Verify initial co-change.
	db := repo.Client().DB()
	var initialCount int
	err = db.QueryRowContext(ctx,
		"SELECT coupling_count FROM co_changed WHERE file_a = ? AND file_b = ?",
		"a.go", "b.go",
	).Scan(&initialCount)
	if err != nil {
		t.Fatalf("query initial co_changed: %v", err)
	}
	if initialCount != 3 {
		t.Errorf("initial coupling_count = %d, want 3", initialCount)
	}

	// Add 2 more commits where a.go and b.go change together.
	for i := 0; i < 2; i++ {
		writeFile(t, repoDir, "a.go", repeated("a-new", i+1))
		writeFile(t, repoDir, "b.go", repeated("b-new", i+1))
		git("add", ".")
		git("commit", "-m", "new pair commit")
	}

	// Incremental index should pick up only the new commits.
	result2, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("EnsureIndex() incremental error = %v", err)
	}
	if result2.NewCommits != 2 {
		t.Errorf("incremental NewCommits = %d, want 2", result2.NewCommits)
	}

	// Verify co-change is updated: 3 + 2 = 5.
	var updatedCount int
	err = db.QueryRowContext(ctx,
		"SELECT coupling_count FROM co_changed WHERE file_a = ? AND file_b = ?",
		"a.go", "b.go",
	).Scan(&updatedCount)
	if err != nil {
		t.Fatalf("query updated co_changed: %v", err)
	}
	if updatedCount != 5 {
		t.Errorf("updated coupling_count = %d, want 5", updatedCount)
	}
}

// repeated returns a string where base is repeated n times, each on its own line.
func repeated(base string, n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += base
	}
	return s
}
