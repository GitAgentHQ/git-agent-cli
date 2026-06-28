package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runGitIn runs git in dir with args, failing the test on error.
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// newCmdWithCtx builds a *cobra.Command whose Context and working directory
// match what runInit passes to untrackGraphDB.
func newCmdWithCtx(t *testing.T, dir string) *cobra.Command {
	t.Helper()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	return cmd
}

// TestUntrackGraphDB_NoOpWhenUntracked locks the contract that untrackGraphDB
// is a safe no-op when .git-agent/graph.db is not tracked: it must not error
// and must not create the file.
func TestUntrackGraphDB_NoOpWhenUntracked(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init", "-q")
	runGitIn(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	c := newCmdWithCtx(t, dir)
	if err := untrackGraphDB(c); err != nil {
		t.Fatalf("untrackGraphDB on untracked repo: %v", err)
	}
	// Must not have created the database.
	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "graph.db")); err == nil {
		t.Error("untrackGraphDB must not create graph.db when it did not exist")
	}
}

// TestUntrackGraphDB_UntracksAndPreservesWorktree locks the contract that a
// tracked .git-agent/graph.db is removed from the index while the working-tree
// file is left intact, and that the IsTracked guard makes the call idempotent.
func TestUntrackGraphDB_UntracksAndPreservesWorktree(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init", "-q")
	runGitIn(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	dbPath := filepath.Join(dir, ".git-agent", "graph.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("sqlite-ish"), 0o644); err != nil {
		t.Fatalf("write graph.db: %v", err)
	}
	runGitIn(t, dir, "add", ".git-agent/graph.db")
	runGitIn(t, dir, "commit", "-q", "-m", "add graph.db")

	c := newCmdWithCtx(t, dir)
	var out bytes.Buffer
	c.SetOut(&out)

	if err := untrackGraphDB(c); err != nil {
		t.Fatalf("untrackGraphDB: %v", err)
	}
	if !strings.Contains(out.String(), "Untracked") {
		t.Errorf("expected 'Untracked ...' message, got: %s", out.String())
	}

	// No longer tracked.
	tracked := trackedFiles(t, dir, ".git-agent/graph.db")
	if len(tracked) != 0 {
		t.Errorf("graph.db should be untracked, got %v", tracked)
	}
	// Working-tree file preserved.
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("working-tree graph.db must still exist: %v", err)
	}

	// Idempotent: a second call is a no-op (no error, no "Untracked" message).
	out.Reset()
	if err := untrackGraphDB(c); err != nil {
		t.Fatalf("second untrackGraphDB: %v", err)
	}
	if strings.Contains(out.String(), "Untracked") {
		t.Errorf("second call should be a no-op, got: %s", out.String())
	}
}

func trackedFiles(t *testing.T, dir, path string) []string {
	t.Helper()
	c := exec.Command("git", "ls-files", "--", path)
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}
