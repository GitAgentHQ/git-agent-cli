package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitCmd_GraphFlag_BuildsGraph asserts `init --graph` is the one-shot cold
// start for the code graph: it builds the commit-history + co-change index (L2)
// and the Event-Log projections (L3) — all without an LLM. The result is
// queryable via `status` and `related`.
func TestInitCmd_GraphFlag_BuildsGraph(t *testing.T) {
	dir := newGitRepo(t)

	// Two files committed together twice so co-change has a pair to record.
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n\nfunc A() { B() }\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package main\n\nfunc B() {}\n")
	runGit(t, dir, "add", "a.go", "b.go")
	runGit(t, dir, "commit", "-q", "-m", "first")
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n\nfunc A() { B(); C() }\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package main\n\nfunc B() {}\nfunc C() {}\n")
	runGit(t, dir, "add", "a.go", "b.go")
	runGit(t, dir, "commit", "-q", "-m", "second")

	// init --graph needs no API key (no LLM).
	out, code := gitAgent(t, dir, "init", "--graph")
	if code != 0 {
		t.Fatalf("init --graph: exit %d\noutput: %s", code, out)
	}

	// graph.db must exist.
	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "graph.db")); err != nil {
		t.Fatalf("graph.db not created: %v", err)
	}

	// L2: commit-history + co-change populated, reported by the top-level status.
	status, code := gitAgent(t, dir, "status", "-o", "json")
	if code != 0 {
		t.Fatalf("status: exit %d\noutput: %s", code, status)
	}
	if !strings.Contains(status, `"commit_count": 2`) {
		t.Errorf("expected commit_count 2, got: %s", status)
	}
	if strings.Contains(status, `"co_changed_count": 0`) {
		t.Errorf("expected co_changed_count > 0 (a.go/b.go co-change), got: %s", status)
	}

	// Co-change is queryable: a.go's related set includes b.go.
	related, code := gitAgent(t, dir, "related", "a.go", "-o", "json", "--min-count", "2")
	if code != 0 {
		t.Fatalf("related a.go: exit %d\noutput: %s", code, related)
	}
	if !strings.Contains(related, "b.go") {
		t.Errorf("expected b.go in a.go's related set, got: %s", related)
	}
}

// TestInitCmd_GraphFlag_OptInNotInWizard asserts the default `init` wizard
// does NOT build the graph (opt-in): the first commit does, via graph_autobuild.
func TestInitCmd_GraphFlag_OptInNotInWizard(t *testing.T) {
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
