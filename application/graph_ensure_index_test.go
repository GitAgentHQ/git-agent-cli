package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	gitinfra "github.com/gitagenthq/git-agent/infrastructure/git"
	graphinfra "github.com/gitagenthq/git-agent/infrastructure/graph"
)

func TestEnsureIndex_CreatesDBWhenMissing(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "main.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "initial commit")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")

	// Verify DB does not exist yet.
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatal("DB should not exist before EnsureIndex")
	}

	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)
	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)

	ctx := context.Background()
	result, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("EnsureIndex() error = %v", err)
	}

	// After openTestDB the DB file exists (created by the test helper),
	// but the EnsureIndex should still run full index since lastHash is empty.
	if result.IndexedCommits != 1 {
		t.Errorf("IndexedCommits = %d, want 1", result.IndexedCommits)
	}

	// Verify DB now exists.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("DB file should exist after EnsureIndex")
	}
}

func TestEnsureIndex_IncrementalWhenStale(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create 3 commits.
	writeFile(t, repoDir, "a.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 1")

	writeFile(t, repoDir, "b.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 2")

	writeFile(t, repoDir, "c.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 3")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()

	// Run full index on these 3 commits.
	result1, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}
	if result1.IndexedCommits != 3 {
		t.Fatalf("FullIndex IndexedCommits = %d, want 3", result1.IndexedCommits)
	}

	// Now add 2 more commits.
	writeFile(t, repoDir, "d.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 4")

	writeFile(t, repoDir, "e.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 5")

	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)
	result2, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("EnsureIndex() error = %v", err)
	}

	// Only the 2 new commits should be indexed.
	if result2.NewCommits != 2 {
		t.Errorf("NewCommits = %d, want 2", result2.NewCommits)
	}

	// Total commits in DB should be 5.
	stats, err := repo.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.CommitCount != 5 {
		t.Errorf("CommitCount = %d, want 5", stats.CommitCount)
	}
}

func TestEnsureIndex_NoOpWhenFresh(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "main.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "initial")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()

	// Full index first.
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	// EnsureIndex should find nothing new.
	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)
	result, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("EnsureIndex() error = %v", err)
	}

	if result.NewCommits != 0 {
		t.Errorf("NewCommits = %d, want 0", result.NewCommits)
	}
}

func TestEnsureIndex_ForcePushTriggersFullReindex(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "a.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 1")

	writeFile(t, repoDir, "b.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 2")

	writeFile(t, repoDir, "c.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 3")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()

	// Full index all 3 commits.
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	// Record the HEAD before a force-push so we can restore history to a real
	// (now-unreachable) ancestor. This exercises the genuine exit-1
	// "is-ancestor false" path, not an invalid-hash exit-128 error.
	preForceHead := strings.TrimSpace(git("rev-parse", "HEAD"))

	// Rewrite history: reset to commit 1, then build a divergent commit 2'.
	// The old commits 2 and 3 become unreachable (exit 1 from merge-base).
	git("reset", "--hard", "HEAD~2")
	writeFile(t, repoDir, "b2.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 2 prime")

	// Point lastIndexedCommit at the old, now-unreachable HEAD (a real commit
	// object, so merge-base --is-ancestor returns exit 1, not an error).
	if err := repo.SetLastIndexedCommit(ctx, preForceHead); err != nil {
		t.Fatalf("SetLastIndexedCommit() error = %v", err)
	}

	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)
	result, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("EnsureIndex() error = %v", err)
	}

	// Full re-index should process all reachable commits: the original commit 1
	// plus the divergent commit 2' (commits 2 and 3 are now unreachable).
	if result.IndexedCommits != 2 {
		t.Errorf("IndexedCommits = %d, want 2", result.IndexedCommits)
	}
}

func TestEnsureIndex_CorruptLastHashSurfacesError(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "a.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 1")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()

	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	// A garbage lastHash is not a force-push: git merge-base exits 128 (real
	// error), which must surface rather than silently destroying the index.
	if err := repo.SetLastIndexedCommit(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); err != nil {
		t.Fatalf("SetLastIndexedCommit() error = %v", err)
	}

	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)
	_, err = ensureSvc.EnsureIndex(ctx, graph.IndexRequest{})
	if err == nil {
		t.Fatal("EnsureIndex() expected error for corrupt lastHash, got nil")
	}
}

func TestEnsureIndex_ForceFlag(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "a.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 1")

	writeFile(t, repoDir, "b.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 2")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)

	ctx := context.Background()

	// Full index first.
	_, err := indexSvc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	// Force=true should trigger full re-index even though DB is fresh.
	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)
	result, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{Force: true})
	if err != nil {
		t.Fatalf("EnsureIndex(Force=true) error = %v", err)
	}

	// Full re-index should reprocess all 2 commits.
	if result.IndexedCommits != 2 {
		t.Errorf("IndexedCommits = %d, want 2", result.IndexedCommits)
	}
}

func TestEnsureIndex_ForceReindexPreservesCaptureHistory(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "a.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "commit 1")

	dbPath := filepath.Join(repoDir, ".git-agent", "graph.db")
	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	indexSvc := NewIndexService(repo, gitClient)
	ctx := context.Background()

	if _, err := indexSvc.FullIndex(ctx, graph.IndexRequest{}); err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}
	if err := repo.UpsertSession(ctx, graph.SessionNode{
		ID:         "session-1",
		Source:     "claude-code",
		InstanceID: "agent-1",
		StartedAt:  100,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := repo.CreateActionBatch(ctx, graph.ActionNode{
		ID:           "session-1:1",
		SessionID:    "session-1",
		Sequence:     1,
		Tool:         "Edit",
		Diff:         "diff --git a/a.go b/a.go\n",
		FilesChanged: []string{"a.go"},
		Timestamp:    101,
	}, []graph.FileChange{{Path: "a.go", Additions: 1}}); err != nil {
		t.Fatalf("CreateActionBatch() error = %v", err)
	}
	if err := repo.UpdateCaptureBaseline(ctx, map[string]string{"a.go": "hash-v1"}); err != nil {
		t.Fatalf("UpdateCaptureBaseline() error = %v", err)
	}
	head := strings.TrimSpace(git("rev-parse", "HEAD"))
	if err := repo.CreateActionProduces(ctx, "session-1:1", head, "a.go"); err != nil {
		t.Fatalf("CreateActionProduces() error = %v", err)
	}

	ensureSvc := NewEnsureIndexService(indexSvc, repo, gitClient, dbPath)
	if _, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{Force: true}); err != nil {
		t.Fatalf("EnsureIndex(Force=true) error = %v", err)
	}

	stats, err := repo.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	baseline, err := repo.GetCaptureBaseline(ctx, []string{"a.go"})
	if err != nil {
		t.Fatalf("GetCaptureBaseline() error = %v", err)
	}

	if stats.ActionCount != 1 || stats.SessionCount != 1 || baseline["a.go"] != "hash-v1" {
		t.Fatalf("force reindex should preserve capture data, got stats=%+v baseline=%v", stats, baseline)
	}
	var linkCount int
	if err := repo.Client().DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM action_produces WHERE action_id = ? AND commit_hash = ? AND file_path = ?`,
		"session-1:1", head, "a.go",
	).Scan(&linkCount); err != nil {
		t.Fatalf("query action_produces: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("force reindex should preserve action_produces link, got %d", linkCount)
	}
}

// openTestDBAt creates a SQLiteClient and SQLiteRepository at a specific path,
// initialises the schema, and registers cleanup.
func openTestDBAt(t *testing.T, dbPath string) *graphinfra.SQLiteRepository {
	t.Helper()
	ctx := context.Background()

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	client := graphinfra.NewSQLiteClient(dbPath)
	if err := client.Open(ctx); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	return graphinfra.NewSQLiteRepository(client)
}
