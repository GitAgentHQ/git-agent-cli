package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInitCmd_WizardDoesNotBuildGraph asserts the default `init` wizard does
// NOT build the graph: the first commit does, via graph_autobuild.
func TestInitCmd_WizardDoesNotBuildGraph(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, "x.txt"), "x\n")
	runGit(t, dir, "add", "x.txt")
	runGit(t, dir, "commit", "-q", "-m", "x")

	// Full wizard without --graph: --gitignore alone runs (no API key needed),
	// and must NOT create graph.db.
	out, code := gitAgent(t, dir, "init", "--gitignore")
	_ = out
	_ = code // gitignore may need network; the point is graph.db must not appear
	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "graph.db")); err == nil {
		t.Errorf("init (no --graph) must not create graph.db; the first commit should")
	}
}
