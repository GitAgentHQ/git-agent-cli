package e2e_test

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture gives two regions far enough apart that git anchors each to its own
// function, which is what makes a region split possible at all.
const stagedBoundarySource = `package app

// Beta renders the beta label.
func Beta() string {
	return "beta"
}

// Gamma renders the gamma label.
func Gamma() string {
	return "gamma"
}

// Delta renders the delta label.
func Delta() string {
	return "delta"
}

// Epsilon renders the epsilon label.
func Epsilon() string {
	return "epsilon"
}
`

// betaOnlyPatch stages the func Beta region and leaves the rest of the change in
// the working tree. It is the exact patch git prints for that one region.
const betaOnlyPatch = `diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -3,7 +3,7 @@ package app
 
 // Beta renders the beta label.
 func Beta() string {
-	return "beta"
+	return "BETA"
 }
 
 // Gamma renders the gamma label.
`

// stagedBoundaryRepo writes the fixture repository and returns its path and the
// path of the patch. The patch lives outside the repository, so the run under test
// never sees it.
func stagedBoundaryRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, "app.go"), stagedBoundarySource)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook:\n  - empty\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "chore(cli): add fixture")

	patch := filepath.Join(t.TempDir(), "beta.patch")
	if err := os.WriteFile(patch, []byte(betaOnlyPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, patch
}

// applyPatch stages one patch into the index.
func applyPatch(t *testing.T, dir, patch string) {
	t.Helper()
	cmd := exec.Command("git", "apply", "--cached", patch)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply: %v\n%s", err, out)
	}
}

// stagedBoundaryChange writes the fixture's two changes into the working tree.
func stagedBoundaryChange(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "app.go"), strings.Replace(
		strings.Replace(stagedBoundarySource, `return "beta"`, `return "BETA"`, 1),
		`return "epsilon"`, `return "EPSILON"`, 1))
}

// TestCommitCmd_StagedBoundary_KeepsTheStagedRegionApart is the defect this fix
// exists for. On develop the run unstages the path and re-adds it whole, so the
// commit carries the working tree change the user had not staged. Here the staged
// region is committed as staged, and the rest follows in its own commit.
func TestCommitCmd_StagedBoundary_KeepsTheStagedRegionApart(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir, patch := stagedBoundaryRepo(t)
	stagedBoundaryChange(t, dir)
	applyPatch(t, dir, patch)

	before := gitOutput(t, dir, "diff", "--cached")
	if strings.Contains(before, "EPSILON") {
		t.Fatalf("the fixture must start with only the func Beta region staged:\n%s", before)
	}

	runCommit(t, dir, server, 0)

	subjects := commitSubjects(t, dir)
	if len(subjects) != 3 {
		t.Fatalf("expected the fixture commit plus 2 commits, got %d: %v", len(subjects), subjects)
	}
	stagedCommit := gitOutput(t, dir, "show", "HEAD~1", "--", "app.go")
	if !strings.Contains(stagedCommit, `+	return "BETA"`) {
		t.Fatalf("the first commit must carry the region the user staged:\n%s", stagedCommit)
	}
	if strings.Contains(stagedCommit, "EPSILON") {
		t.Fatalf("the first commit must not carry the working tree change the user had not staged:\n%s", stagedCommit)
	}
	restCommit := gitOutput(t, dir, "show", "HEAD", "--", "app.go")
	if !strings.Contains(restCommit, `+	return "EPSILON"`) || strings.Contains(restCommit, "BETA") {
		t.Fatalf("the second commit must carry the remainder alone:\n%s", restCommit)
	}
	if status := gitOutput(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("every region must be committed:\n%s", status)
	}
}

// TestCommitCmd_StagedBoundary_NoStageCommitsTheIndexOnly covers the flag whose
// help already promises this: only what is staged is committed, and the working
// tree keeps the rest.
func TestCommitCmd_StagedBoundary_NoStageCommitsTheIndexOnly(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir, patch := stagedBoundaryRepo(t)
	stagedBoundaryChange(t, dir)
	applyPatch(t, dir, patch)

	runCommit(t, dir, server, 0, "--no-stage")

	if got := len(commitSubjects(t, dir)); got != 2 {
		t.Fatalf("expected the fixture commit plus one commit, got %d: %v", got, commitSubjects(t, dir))
	}
	head := gitOutput(t, dir, "show", "HEAD", "--", "app.go")
	if !strings.Contains(head, `+	return "BETA"`) || strings.Contains(head, "EPSILON") {
		t.Fatalf("--no-stage must commit only the index content:\n%s", head)
	}
	if staged := gitOutput(t, dir, "diff", "--cached"); strings.TrimSpace(staged) != "" {
		t.Fatalf("the index must be empty after the commit:\n%s", staged)
	}
	if loose := gitOutput(t, dir, "diff", "--", "app.go"); !strings.Contains(loose, `+	return "EPSILON"`) {
		t.Fatalf("the working tree must still hold the region the user did not stage:\n%s", loose)
	}
}

// TestCommitCmd_StagedBoundary_ReportsAMultiRegionFile proves the run says when a
// file holds more than one region, and names the command that splits it. It
// reports; it never splits on its own.
func TestCommitCmd_StagedBoundary_ReportsAMultiRegionFile(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir, _ := stagedBoundaryRepo(t)
	stagedBoundaryChange(t, dir)

	_, stderr, code := gitAgentSeparated(t, dir, "commit",
		"--api-key", "test-key", "--base-url", server.URL, "--model", "test-model")
	if code != 0 {
		t.Fatalf("commit exited %d:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "holds 2 separate regions") {
		t.Fatalf("the run must report the regions a file holds, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "git add -p app.go") {
		t.Fatalf("the report must name the command that splits it, got:\n%s", stderr)
	}
	// One region per commit unless the user asks otherwise: the run reports, and
	// commits the file as one.
	if got := len(commitSubjects(t, dir)); got != 2 {
		t.Fatalf("the run must not split on its own, got %d commits: %v", got, commitSubjects(t, dir))
	}
}

// TestCommitCmd_StagedBoundary_SingleRegionIsNotReported keeps the report tied to
// a file that really holds more than one region.
func TestCommitCmd_StagedBoundary_SingleRegionIsNotReported(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir, _ := stagedBoundaryRepo(t)
	writeFile(t, filepath.Join(dir, "app.go"),
		strings.Replace(stagedBoundarySource, `return "beta"`, `return "BETA"`, 1))

	_, stderr, code := gitAgentSeparated(t, dir, "commit",
		"--api-key", "test-key", "--base-url", server.URL, "--model", "test-model")
	if code != 0 {
		t.Fatalf("commit exited %d:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "separate regions") {
		t.Fatalf("a single region must not be reported, got:\n%s", stderr)
	}
}

// gitOutput runs one git command in a repository and fails the test on error.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// commitSubjects returns the subjects of a repository's commits, newest first.
func commitSubjects(t *testing.T, dir string) []string {
	t.Helper()
	raw := gitOutput(t, dir, "log", "--format=%s")
	var out []string
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// runCommit runs the CLI against the stub endpoint and requires the exit code.
func runCommit(t *testing.T, dir string, server *httptest.Server, want int, args ...string) {
	t.Helper()
	full := append([]string{"commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
	}, args...)
	out, code := gitAgent(t, dir, full...)
	if code != want {
		t.Fatalf("git-agent commit exited %d, want %d:\n%s", code, want, out)
	}
}
