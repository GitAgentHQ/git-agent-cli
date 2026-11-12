package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

var multiRegionFiles = []string{"a.go", "b.go", "c.go", "d.go"}

// multiRegionRepo commits four copies of the staged-boundary fixture, then
// changes two distant regions in every copy. Git reports two regions per file,
// which is the smallest CLI-level version of the reported warning flood.
func multiRegionRepo(t *testing.T) string {
	t.Helper()
	dir := newGitRepo(t)
	changed := strings.Replace(
		strings.Replace(stagedBoundarySource, "return \"beta\"", "return \"BETA\"", 1),
		"return \"epsilon\"", "return \"EPSILON\"", 1)
	for _, file := range multiRegionFiles {
		writeFile(t, filepath.Join(dir, file), stagedBoundarySource)
	}
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook:\n  - empty\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "chore(cli): add fixture")
	for _, file := range multiRegionFiles {
		writeFile(t, filepath.Join(dir, file), changed)
	}
	return dir
}

func multiRegionCommitArgs(server string, verbose bool) []string {
	args := []string{"commit",
		"--api-key", "test-key",
		"--base-url", server,
		"--model", "test-model",
	}
	if verbose {
		args = append(args, "--verbose")
	}
	return args
}

func TestCommitCmd_ManyMultiRegionFiles_BoundedWarning(t *testing.T) {
	server := newAutoInitLLMServer(t)
	defer server.Close()

	t.Run("normal run prints one summary", func(t *testing.T) {
		dir := multiRegionRepo(t)
		_, stderr, code := gitAgentSeparated(t, dir, multiRegionCommitArgs(server.URL, false)...)
		if code != 0 {
			t.Fatalf("commit exited %d:\n%s", code, stderr)
		}
		if !strings.Contains(stderr, "4 files hold separate regions") {
			t.Fatalf("missing bounded summary:\n%s", stderr)
		}
		if !strings.Contains(stderr, "git add -p a.go") {
			t.Fatalf("summary must name a concrete command:\n%s", stderr)
		}
		if got := strings.Count(stderr, "git add -p "); got != 1 {
			t.Fatalf("want exactly one git add -p command, got %d:\n%s", got, stderr)
		}
		if !strings.Contains(stderr, "--verbose") {
			t.Fatalf("summary must point at --verbose:\n%s", stderr)
		}
		if got := strings.Count(stderr, "separate regions"); got != 1 {
			t.Fatalf("want exactly one region warning, got %d:\n%s", got, stderr)
		}
	})

	t.Run("verbose run lists every file", func(t *testing.T) {
		dir := multiRegionRepo(t)
		_, stderr, code := gitAgentSeparated(t, dir, multiRegionCommitArgs(server.URL, true)...)
		if code != 0 {
			t.Fatalf("verbose commit exited %d:\n%s", code, stderr)
		}
		if !strings.Contains(stderr, "4 files hold separate regions") {
			t.Fatalf("verbose run must keep the bounded summary:\n%s", stderr)
		}
		if strings.Contains(stderr, "run with --verbose") {
			t.Fatalf("verbose summary must not repeat the flag hint:\n%s", stderr)
		}
		summaryAt := strings.Index(stderr, "4 files hold separate regions")
		detailAt := strings.Index(stderr, "holds 2 separate regions")
		if summaryAt < 0 || detailAt < 0 || summaryAt > detailAt {
			t.Fatalf("summary must appear before the per-file details:\n%s", stderr)
		}
		for _, file := range multiRegionFiles {
			if !strings.Contains(stderr, "git add -p "+file) {
				t.Fatalf("verbose details missing %s:\n%s", file, stderr)
			}
		}
		if got := strings.Count(stderr, "holds 2 separate regions"); got != 4 {
			t.Fatalf("want four per-file detail lines, got %d:\n%s", got, stderr)
		}
	})
}
