package application

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	gitinfra "github.com/gitagenthq/git-agent/infrastructure/git"
	graphinfra "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// testRepo creates a temporary git repository, returns the path and a helper
// to run git commands inside it. The returned env vars ensure deterministic
// author/committer identity.
func testRepo(t *testing.T) (string, func(args ...string) string) {
	t.Helper()
	dir := t.TempDir()

	env := []string{
		"GIT_AUTHOR_NAME=Test Author",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test Author",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"HOME=" + dir,
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
		return string(out)
	}

	run("init")
	run("config", "user.name", "Test Author")
	run("config", "user.email", "test@example.com")

	return dir, run
}

// openTestDB creates a SQLiteClient and SQLiteRepository in the given repo dir,
// initialises the schema, and registers cleanup.
func openTestDB(t *testing.T, repoDir string) *graphinfra.SQLiteRepository {
	t.Helper()
	ctx := context.Background()

	agentDir := filepath.Join(repoDir, ".git-agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("mkdir .git-agent: %v", err)
	}

	client := graphinfra.NewSQLiteClient(filepath.Join(agentDir, "graph.db"))
	if err := client.Open(ctx); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	return graphinfra.NewSQLiteRepository(client)
}

func TestIndexService_FullIndex(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create 3 commits touching 4 files total.
	writeFile(t, repoDir, "main.go", "package main\n")
	writeFile(t, repoDir, "README.md", "# project\n")
	git("add", ".")
	git("commit", "-m", "initial commit")

	writeFile(t, repoDir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, repoDir, "util.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "add util and update main")

	writeFile(t, repoDir, "config.yml", "key: value\n")
	git("add", ".")
	git("commit", "-m", "add config")

	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	svc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	result, err := svc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	if result.IndexedCommits != 3 {
		t.Errorf("IndexedCommits = %d, want 3", result.IndexedCommits)
	}
	if result.Files < 4 {
		t.Errorf("Files = %d, want >= 4", result.Files)
	}
	if result.Authors != 1 {
		t.Errorf("Authors = %d, want 1", result.Authors)
	}
	if result.LastCommit == "" {
		t.Error("LastCommit is empty")
	}

	// Verify rows in the database via stats.
	stats, err := repo.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.CommitCount != 3 {
		t.Errorf("stats.CommitCount = %d, want 3", stats.CommitCount)
	}
	if stats.AuthorCount != 1 {
		t.Errorf("stats.AuthorCount = %d, want 1", stats.AuthorCount)
	}
	if stats.FileCount < 4 {
		t.Errorf("stats.FileCount = %d, want >= 4", stats.FileCount)
	}
}

func TestIndexService_FullIndex_Renames(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "old.txt", "content\n")
	git("add", ".")
	git("commit", "-m", "add old.txt")

	git("mv", "old.txt", "new.txt")
	git("commit", "-m", "rename old to new")

	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	svc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	result, err := svc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	if result.IndexedCommits != 2 {
		t.Errorf("IndexedCommits = %d, want 2", result.IndexedCommits)
	}

	// Verify renames table has an entry.
	db := repo.Client().DB()
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM renames").Scan(&count)
	if err != nil {
		t.Fatalf("count renames: %v", err)
	}
	if count == 0 {
		t.Error("renames table is empty, expected at least 1 entry")
	}
}

func TestIndexService_FullIndex_SkipsLargeCommits(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create a commit with 60 files.
	for i := 0; i < 60; i++ {
		writeFile(t, repoDir, fmt.Sprintf("file_%03d.txt", i), fmt.Sprintf("content %d\n", i))
	}
	git("add", ".")
	git("commit", "-m", "bulk add 60 files")

	// Create a small commit.
	writeFile(t, repoDir, "small.txt", "small\n")
	git("add", ".")
	git("commit", "-m", "add small file")

	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	svc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	result, err := svc.FullIndex(ctx, graph.IndexRequest{MaxFilesPerCommit: 50})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	// Both commits should be counted as indexed.
	if result.IndexedCommits != 2 {
		t.Errorf("IndexedCommits = %d, want 2", result.IndexedCommits)
	}

	// But the large commit's files should not generate modifies rows.
	db := repo.Client().DB()

	// The bulk commit is the first (oldest). Get its hash.
	var bulkHash string
	err = db.QueryRowContext(ctx,
		"SELECT hash FROM commits ORDER BY timestamp ASC LIMIT 1",
	).Scan(&bulkHash)
	if err != nil {
		t.Fatalf("get bulk hash: %v", err)
	}

	var modCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM modifies WHERE commit_hash = ?", bulkHash,
	).Scan(&modCount)
	if err != nil {
		t.Fatalf("count modifies for bulk: %v", err)
	}
	if modCount != 0 {
		t.Errorf("modifies for large commit = %d, want 0", modCount)
	}

	// The small commit should have its modifies.
	var smallMod int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM modifies WHERE commit_hash != ?", bulkHash,
	).Scan(&smallMod)
	if err != nil {
		t.Fatalf("count modifies for small: %v", err)
	}
	if smallMod == 0 {
		t.Error("small commit should have modifies entries")
	}
}

func TestIndexService_FullIndex_MaxCommits(t *testing.T) {
	repoDir, git := testRepo(t)

	// Create 10 commits.
	for i := 0; i < 10; i++ {
		writeFile(t, repoDir, fmt.Sprintf("file_%d.txt", i), fmt.Sprintf("v%d\n", i))
		git("add", ".")
		git("commit", "-m", fmt.Sprintf("commit %d", i))
	}

	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	svc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	result, err := svc.FullIndex(ctx, graph.IndexRequest{MaxCommits: 5})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	if result.IndexedCommits != 5 {
		t.Errorf("IndexedCommits = %d, want 5", result.IndexedCommits)
	}

	stats, err := repo.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.CommitCount != 5 {
		t.Errorf("stats.CommitCount = %d, want 5", stats.CommitCount)
	}
}

func TestIndexService_FullIndex_StoresLastIndexedCommit(t *testing.T) {
	repoDir, git := testRepo(t)

	writeFile(t, repoDir, "a.txt", "a\n")
	git("add", ".")
	git("commit", "-m", "first")

	writeFile(t, repoDir, "b.txt", "b\n")
	git("add", ".")
	git("commit", "-m", "second")

	repo := openTestDB(t, repoDir)
	gitClient := gitinfra.NewGraphClient(repoDir)
	svc := NewIndexService(repo, gitClient)

	ctx := context.Background()
	result, err := svc.FullIndex(ctx, graph.IndexRequest{})
	if err != nil {
		t.Fatalf("FullIndex() error = %v", err)
	}

	stored, err := repo.GetLastIndexedCommit(ctx)
	if err != nil {
		t.Fatalf("GetLastIndexedCommit() error = %v", err)
	}
	if stored == "" {
		t.Fatal("last indexed commit is empty")
	}
	if stored != result.LastCommit {
		t.Errorf("stored last commit %q != result.LastCommit %q", stored, result.LastCommit)
	}

	// The stored hash should be the HEAD of the repo.
	head, err := gitClient.CurrentHead(ctx)
	if err != nil {
		t.Fatalf("CurrentHead() error = %v", err)
	}
	if stored != head {
		t.Errorf("stored last commit %q != HEAD %q", stored, head)
	}
}

// writeFile creates a file with the given content inside the repo directory.
func writeFile(t *testing.T, repoDir, name, content string) {
	t.Helper()
	path := filepath.Join(repoDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
