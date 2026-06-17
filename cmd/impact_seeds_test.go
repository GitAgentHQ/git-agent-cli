package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
)

func TestIsToolingPath(t *testing.T) {
	for _, p := range []string{".git-agent", ".git-agent/graph.db", ".claude/settings.json"} {
		if !isToolingPath(p) {
			t.Errorf("%q should be a tooling path", p)
		}
	}
	for _, p := range []string{"main.go", "src/.claude.go", "git-agent/x"} {
		if isToolingPath(p) {
			t.Errorf("%q should NOT be a tooling path", p)
		}
	}
}

// seedTestRepo builds a temp git repo with a couple of committed files under a
// subdirectory and returns its root.
func seedTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t.com", "HOME="+dir)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	mustWrite(t, dir, "pkg/a.go", "package pkg\n")
	mustWrite(t, dir, "pkg/b.go", "package pkg\n")
	mustWrite(t, dir, "main.go", "package main\n")
	run("add", "-A")
	run("commit", "-m", "init")
	return dir
}

func mustWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSeeds_DirectoryExpands(t *testing.T) {
	root := seedTestRepo(t)
	graphGit := infraGit.NewGraphClient(root)

	seeds, err := resolveSeeds(context.Background(), []string{"pkg"}, root, root, graphGit)
	if err != nil {
		t.Fatalf("resolveSeeds: %v", err)
	}
	got := map[string]bool{}
	for _, s := range seeds {
		got[s] = true
	}
	if !got["pkg/a.go"] || !got["pkg/b.go"] {
		t.Errorf("directory should expand to pkg/a.go and pkg/b.go, got %v", seeds)
	}
	if got["main.go"] {
		t.Errorf("main.go is outside pkg/ and must not be a seed: %v", seeds)
	}
}

func TestResolveSeeds_WorkingTreeAndToolingExclusion(t *testing.T) {
	root := seedTestRepo(t)
	graphGit := infraGit.NewGraphClient(root)

	// Edit a real file and create tooling churn that must be ignored.
	mustWrite(t, root, "main.go", "package main\n// edit\n")
	mustWrite(t, root, ".git-agent/graph.db", "binary")
	mustWrite(t, root, ".claude/settings.json", "{}")

	seeds, err := resolveSeeds(context.Background(), nil, root, root, graphGit)
	if err != nil {
		t.Fatalf("resolveSeeds: %v", err)
	}
	for _, s := range seeds {
		if isToolingPath(s) {
			t.Errorf("tooling path %q must not be a seed", s)
		}
	}
	found := false
	for _, s := range seeds {
		if s == "main.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("working-tree mode should seed the edited main.go, got %v", seeds)
	}
}
