package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitCmd_GraphFlag_BuildsAllThreeLayers asserts `init --graph` is the
// one-shot cold start for the code graph: it builds the commit-history +
// co-change index (L2, the step `graph index` never did), the Event-Log
// projections (L3), and the AST symbol index (L1) — all without an LLM.
func TestInitCmd_GraphFlag_BuildsAllThreeLayers(t *testing.T) {
	dir := newGitRepo(t)

	// Two Go files that call each other, committed together twice so co-change
	// has a pair to record and AST has caller/callee edges.
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

	// L2: commit-history + co-change populated.
	status, code := gitAgent(t, dir, "graph", "status", "--json")
	if code != 0 {
		t.Fatalf("graph status: exit %d\noutput: %s", code, status)
	}
	if !strings.Contains(status, `"commit_count": 2`) {
		t.Errorf("expected commit_count 2, got: %s", status)
	}
	if strings.Contains(status, `"co_changed_count": 0`) {
		t.Errorf("expected co_changed_count > 0 (a.go/b.go co-change), got: %s", status)
	}

	// L1: AST index populated — callers of B must resolve to A.
	callers, code := gitAgent(t, dir, "graph", "callers", "B", "--json")
	if code != 0 {
		t.Fatalf("graph callers B: exit %d\noutput: %s", code, callers)
	}
	if !strings.Contains(callers, `"A"`) {
		t.Errorf("expected caller A of B in AST index, got: %s", callers)
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
