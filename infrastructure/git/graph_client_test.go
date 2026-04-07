package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// initTestRepo creates a git repo in a temp dir, configures author identity,
// and returns the path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "Test Author"},
		{"git", "config", "user.email", "test@example.com"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}
	return dir
}

// testEnv returns a clean git environment with deterministic timestamps.
func testEnv(epoch int) []string {
	dateStr := formatEpochDate(epoch)
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_DATE="+dateStr,
		"GIT_COMMITTER_DATE="+dateStr,
	)
}

// formatEpochDate returns an ISO date string with hours derived from epoch seconds.
func formatEpochDate(epoch int) string {
	h := epoch / 3600
	m := (epoch % 3600) / 60
	s := epoch % 60
	return "2025-01-01T" + zeroPad(h) + ":" + zeroPad(m) + ":" + zeroPad(s) + "+00:00"
}

func zeroPad(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// commitFile writes content, stages it, and commits. Returns the commit hash.
func commitFile(t *testing.T, repoDir, path, content, message string, epoch int) string {
	t.Helper()
	full := filepath.Join(repoDir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	env := testEnv(epoch)
	for _, args := range [][]string{
		{"git", "add", path},
		{"git", "commit", "-m", message},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}
	return getHead(t, repoDir)
}

// removeFile removes a tracked file, stages deletion, and commits.
func removeFile(t *testing.T, repoDir, path, message string, epoch int) string {
	t.Helper()
	env := testEnv(epoch)
	for _, args := range [][]string{
		{"git", "rm", path},
		{"git", "commit", "-m", message},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}
	return getHead(t, repoDir)
}

// renameFile performs a git mv and commits. Returns the commit hash.
func renameFile(t *testing.T, repoDir, oldPath, newPath, message string, epoch int) string {
	t.Helper()
	full := filepath.Join(repoDir, newPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	env := testEnv(epoch)
	for _, args := range [][]string{
		{"git", "mv", oldPath, newPath},
		{"git", "commit", "-m", message},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}
	return getHead(t, repoDir)
}

func getHead(t *testing.T, repoDir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// findCommit locates a commit by hash in the slice.
func findCommit(commits []graph.CommitInfo, hash string) *graph.CommitInfo {
	for i := range commits {
		if commits[i].Hash == hash {
			return &commits[i]
		}
	}
	return nil
}

func TestGraphClient_CommitLogDetailed(t *testing.T) {
	dir := initTestRepo(t)

	hash1 := commitFile(t, dir, "file1.go", "package main\n", "feat: add file1", 3600)
	hash2 := commitFile(t, dir, "file2.go", "package lib\n", "feat: add file2", 7200)
	hash3 := commitFile(t, dir, "file1.go", "package main\n\nfunc init() {}\n", "fix: update file1", 10800)

	gc := NewGraphClient(dir)
	commits, err := gc.CommitLogDetailed(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 3 {
		t.Fatalf("expected 3 commits, got %d", len(commits))
	}

	// Commits are returned newest-first (git log default).
	c3 := commits[0]
	c2 := commits[1]
	c1 := commits[2]

	// Verify hashes.
	if c1.Hash != hash1 {
		t.Errorf("commit 1 hash = %q, want %q", c1.Hash, hash1)
	}
	if c2.Hash != hash2 {
		t.Errorf("commit 2 hash = %q, want %q", c2.Hash, hash2)
	}
	if c3.Hash != hash3 {
		t.Errorf("commit 3 hash = %q, want %q", c3.Hash, hash3)
	}

	// Verify messages.
	if c1.Message != "feat: add file1" {
		t.Errorf("commit 1 message = %q", c1.Message)
	}

	// Verify author.
	if c1.AuthorName != "Test Author" {
		t.Errorf("author name = %q", c1.AuthorName)
	}
	if c1.AuthorEmail != "test@example.com" {
		t.Errorf("author email = %q", c1.AuthorEmail)
	}

	// Verify timestamp is nonzero.
	if c1.Timestamp == 0 {
		t.Error("timestamp should not be zero")
	}

	// Verify parent hashes.
	if len(c1.ParentHashes) != 0 {
		t.Errorf("first commit should have no parents, got %v", c1.ParentHashes)
	}
	if len(c2.ParentHashes) != 1 || c2.ParentHashes[0] != hash1 {
		t.Errorf("commit 2 parents = %v, want [%s]", c2.ParentHashes, hash1)
	}

	// Verify file changes.
	if len(c1.Files) != 1 {
		t.Fatalf("commit 1 files: got %d, want 1", len(c1.Files))
	}
	if c1.Files[0].Path != "file1.go" || c1.Files[0].Status != "A" {
		t.Errorf("commit 1 file = %+v", c1.Files[0])
	}

	if len(c3.Files) != 1 {
		t.Fatalf("commit 3 files: got %d, want 1", len(c3.Files))
	}
	if c3.Files[0].Path != "file1.go" || c3.Files[0].Status != "M" {
		t.Errorf("commit 3 file = %+v", c3.Files[0])
	}

	// Verify additions are populated for new files.
	if c1.Files[0].Additions == 0 {
		t.Error("commit 1 file should have additions > 0")
	}
}

func TestGraphClient_CommitLogDetailed_Renames(t *testing.T) {
	dir := initTestRepo(t)

	commitFile(t, dir, "old_name.go", "package main\n", "feat: add file", 3600)
	renameHash := renameFile(t, dir, "old_name.go", "new_name.go", "refactor: rename file", 7200)

	gc := NewGraphClient(dir)
	commits, err := gc.CommitLogDetailed(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	rc := findCommit(commits, renameHash)
	if rc == nil {
		t.Fatal("rename commit not found")
	}

	if len(rc.Files) < 1 {
		t.Fatal("rename commit should have file changes")
	}

	fc := rc.Files[0]
	if fc.Status != "R" {
		t.Errorf("status = %q, want R", fc.Status)
	}
	if fc.OldPath != "old_name.go" {
		t.Errorf("old path = %q, want old_name.go", fc.OldPath)
	}
	if fc.Path != "new_name.go" {
		t.Errorf("path = %q, want new_name.go", fc.Path)
	}
}

func TestGraphClient_CommitLogDetailed_SinceHash(t *testing.T) {
	dir := initTestRepo(t)

	hash1 := commitFile(t, dir, "a.go", "a\n", "first", 3600)
	commitFile(t, dir, "b.go", "b\n", "second", 7200)
	commitFile(t, dir, "c.go", "c\n", "third", 10800)

	gc := NewGraphClient(dir)
	commits, err := gc.CommitLogDetailed(context.Background(), hash1, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits after sinceHash, got %d", len(commits))
	}

	for _, c := range commits {
		if c.Hash == hash1 {
			t.Error("sinceHash commit should not be included")
		}
	}
}

func TestGraphClient_CommitLogDetailed_MaxCommits(t *testing.T) {
	dir := initTestRepo(t)

	commitFile(t, dir, "a.go", "a\n", "first", 3600)
	commitFile(t, dir, "b.go", "b\n", "second", 7200)
	commitFile(t, dir, "c.go", "c\n", "third", 10800)

	gc := NewGraphClient(dir)
	commits, err := gc.CommitLogDetailed(context.Background(), "", 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits (max), got %d", len(commits))
	}
}

func TestGraphClient_CurrentHead(t *testing.T) {
	dir := initTestRepo(t)

	expected := commitFile(t, dir, "main.go", "package main\n", "init", 3600)

	gc := NewGraphClient(dir)
	head, err := gc.CurrentHead(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if head != expected {
		t.Errorf("CurrentHead() = %q, want %q", head, expected)
	}
}

func TestGraphClient_MergeBaseIsAncestor_True(t *testing.T) {
	dir := initTestRepo(t)

	ancestor := commitFile(t, dir, "a.go", "a\n", "first", 3600)
	head := commitFile(t, dir, "b.go", "b\n", "second", 7200)

	gc := NewGraphClient(dir)
	ok, err := gc.MergeBaseIsAncestor(context.Background(), ancestor, head)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected ancestor to be an ancestor of head")
	}
}

func TestGraphClient_MergeBaseIsAncestor_False(t *testing.T) {
	dir := initTestRepo(t)

	commitFile(t, dir, "a.go", "a\n", "first", 3600)
	head := commitFile(t, dir, "b.go", "b\n", "second", 7200)

	// Create a side branch from the first commit.
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	cmd := exec.Command("git", "checkout", "-b", "side", "HEAD~1")
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout failed: %s", out)
	}

	sideHash := commitFile(t, dir, "side.go", "side\n", "side commit", 10800)

	gc := NewGraphClient(dir)
	ok, err := gc.MergeBaseIsAncestor(context.Background(), sideHash, head)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected non-ancestor relationship, got true")
	}
}

func TestGraphClient_HashObject(t *testing.T) {
	dir := initTestRepo(t)

	content := "package main\n"
	fpath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(fpath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Get expected hash from git directly.
	cmd := exec.Command("git", "hash-object", "main.go")
	cmd.Dir = dir
	expected, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	gc := NewGraphClient(dir)
	hash, err := gc.HashObject(context.Background(), "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if hash != strings.TrimSpace(string(expected)) {
		t.Errorf("HashObject() = %q, want %q", hash, strings.TrimSpace(string(expected)))
	}
}

func TestGraphClient_DiffNameOnly(t *testing.T) {
	dir := initTestRepo(t)

	commitFile(t, dir, "tracked.go", "original\n", "init", 3600)

	// Modify tracked file (unstaged change).
	if err := os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stage a new file.
	if err := os.WriteFile(filepath.Join(dir, "staged.go"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "staged.go")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s", out)
	}

	gc := NewGraphClient(dir)
	files, err := gc.DiffNameOnly(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(files)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %v", files)
	}
	if files[0] != "staged.go" || files[1] != "tracked.go" {
		t.Errorf("files = %v, want [staged.go tracked.go]", files)
	}
}

func TestGraphClient_DiffForFiles(t *testing.T) {
	dir := initTestRepo(t)

	commitFile(t, dir, "a.go", "original a\n", "init a", 3600)
	commitFile(t, dir, "b.go", "original b\n", "init b", 7200)

	// Modify both files.
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("modified a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("modified b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gc := NewGraphClient(dir)

	// Request diff for only a.go.
	diffOut, err := gc.DiffForFiles(context.Background(), []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(diffOut, "a.go") {
		t.Error("diff should contain a.go")
	}
	if strings.Contains(diffOut, "b.go") {
		t.Error("diff should not contain b.go when only a.go requested")
	}
}
