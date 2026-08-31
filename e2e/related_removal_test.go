package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoricalAnalysisCommandsAreUnavailable(t *testing.T) {
	dir := newGitRepo(t)
	for _, command := range []string{"related", "status"} {
		out, code := gitAgent(t, dir, command)
		if code == 0 || !strings.Contains(out, "unknown command") {
			t.Fatalf("%s must report an unknown command, got exit %d: %s", command, code, out)
		}
	}
}

func TestCommitDoesNotCreateGraphDatabase(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()
	dir := newGitRepo(t)

	commitOneFile(t, dir, server.URL, "scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")

	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "graph.db")); !os.IsNotExist(err) {
		t.Fatalf("commit must not create a graph database; stat error: %v", err)
	}
}
