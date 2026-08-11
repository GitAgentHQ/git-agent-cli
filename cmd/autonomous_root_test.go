package cmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/cmd"
)

func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, out)
	}
}

func TestAutonomousRoot_CleanRepo(t *testing.T) {
	cmd.ResetRootFlags()
	defer cmd.ResetRootFlags()
	dir := setupTestGitRepo(t)
	// Create .gitignore so auto-gitignore won't trigger network call to toptal
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.tmp\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create project config with scope so auto-scope won't trigger LLM
	cfgPath := filepath.Join(dir, ".git-agent")
	if err := os.MkdirAll(cfgPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgPath, "config.yml"), []byte("scopes:\n  - name: main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err := cmd.ExecuteArgs([]string{"--base-url", "http://localhost:8080", "--api-key", "test-key", "--model", "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error running bare git-agent on clean repo: %v", err)
	}
}

func TestAutonomousRoot_DryRun_CleanRepo(t *testing.T) {
	cmd.ResetRootFlags()
	defer cmd.ResetRootFlags()
	dir := setupTestGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err := cmd.ExecuteArgs([]string{"--dry-run", "--base-url", "http://localhost:8080", "--api-key", "test-key", "--model", "gpt-4o"})
	if err != nil {
		t.Fatalf("expected git-agent --dry-run on clean repo to exit 0, got: %v", err)
	}
}
