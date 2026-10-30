package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// These tests compare this branch with the develop revision, so the comparison is
// a test rather than a note. The develop binary is built once from a detached
// worktree of that revision and reused.

var (
	developBinOnce sync.Once
	developBin     string
	developBinErr  error
)

// developBinary builds the develop revision and returns the binary path. It skips
// when the revision is not available, which is what a shallow checkout sees.
func developBinary(t *testing.T) string {
	t.Helper()
	developBinOnce.Do(func() {
		root, err := repoRootOf()
		if err != nil {
			developBinErr = fmt.Errorf("locating the repository: %w", err)
			return
		}
		if err := exec.Command("git", "-C", root, "rev-parse", "--verify", "develop").Run(); err != nil {
			developBinErr = fmt.Errorf("the develop revision is not in this checkout")
			return
		}
		dir, err := os.MkdirTemp("", "git-agent-develop-*")
		if err != nil {
			developBinErr = err
			return
		}
		// The binary is written outside the checkout, so the worktree can be
		// removed again and the repository keeps no stale registration.
		binDir, err := os.MkdirTemp("", "git-agent-develop-bin-*")
		if err != nil {
			developBinErr = err
			return
		}
		developBin = filepath.Join(binDir, "git-agent")
		if out, err := runGitIn(root, "worktree", "add", "--detach", dir, "develop"); err != nil {
			developBinErr = fmt.Errorf("worktree add: %v\n%s", err, out)
			return
		}
		defer func() {
			_, _ = runGitIn(root, "worktree", "remove", "--force", dir)
			_ = os.RemoveAll(dir)
		}()
		build := exec.Command("go", "build", "-o", developBin, ".")
		build.Dir = dir
		if out, err := build.CombinedOutput(); err != nil {
			developBinErr = fmt.Errorf("building develop: %v\n%s", err, out)
			return
		}
	})
	if developBinErr != nil {
		t.Skipf("cannot compare with develop: %v", developBinErr)
	}
	return developBin
}

func repoRootOf() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runGitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runBinary runs one binary in dir and returns its stdout, stderr and exit code.
func runBinary(t *testing.T, bin, dir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running %s: %v", bin, err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// TestCommitCmd_StagedBoundary_DevelopAbsorbsWhatTheBranchKeepsApart is the
// comparison this fix exists for, run against both binaries on the same fixture:
// one region staged by hand, the other loose in the working tree.
func TestCommitCmd_StagedBoundary_DevelopAbsorbsWhatTheBranchKeepsApart(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()
	develop := developBinary(t)

	args := []string{"commit", "--api-key", "test-key", "--base-url", server.URL,
		"--model", "test-model", "--no-stage"}

	developDir, developPatch := stagedBoundaryRepo(t)
	stagedBoundaryChange(t, developDir)
	applyPatch(t, developDir, developPatch)
	if _, stderr, code := runBinary(t, develop, developDir, args...); code != 0 {
		t.Fatalf("the develop binary failed with %d:\n%s", code, stderr)
	}
	developHead := gitOutput(t, developDir, "show", "HEAD", "--", "app.go")
	if !strings.Contains(developHead, "EPSILON") {
		t.Fatalf("the fixture no longer reproduces the develop defect:\n%s", developHead)
	}
	if loose := gitOutput(t, developDir, "diff", "--name-only"); strings.TrimSpace(loose) != "" {
		t.Fatalf("develop left work behind, so it did not absorb it:\n%s", loose)
	}
	t.Logf("develop: the staged commit absorbed the unstaged region, and the working tree was left empty")

	branchDir, branchPatch := stagedBoundaryRepo(t)
	stagedBoundaryChange(t, branchDir)
	applyPatch(t, branchDir, branchPatch)
	if _, stderr, code := runBinary(t, agentBin, branchDir, args...); code != 0 {
		t.Fatalf("this branch failed with %d:\n%s", code, stderr)
	}
	branchHead := gitOutput(t, branchDir, "show", "HEAD", "--", "app.go")
	if strings.Contains(branchHead, "EPSILON") {
		t.Fatalf("this branch must not absorb the unstaged region:\n%s", branchHead)
	}
	if !strings.Contains(branchHead, `+	return "BETA"`) {
		t.Fatalf("this branch must commit the staged region:\n%s", branchHead)
	}
	if loose := gitOutput(t, branchDir, "diff", "--name-only"); !strings.Contains(loose, "app.go") {
		t.Fatalf("this branch must leave the unstaged region in the working tree, got %q", loose)
	}
}

// capturingStub records the request bodies it is asked to answer, so two runs can
// be compared on the prompts they sent as well as on what they committed.
type capturingStub struct {
	server *httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newCapturingStub(t *testing.T) *capturingStub {
	t.Helper()
	stub := &capturingStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.bodies = append(stub.bodies, string(raw))
		stub.mu.Unlock()

		content := `{"title":"feat(cli): initial commit","bullets":["Change the files"],"explanation":"Changes the files."}`
		if strings.Contains(string(raw), "meaningful atomic commits") {
			content = planFromPrompt(string(raw))
		}
		payload, _ := json.Marshal(content)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"x","object":"chat.completion","created":0,"model":"test-model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%s}}]}`, payload)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *capturingStub) captured() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// emptyIndexFixture is a repository with two changed files and nothing staged, so
// the run takes the file-level path and asks the planner once.
func emptyIndexFixture(t *testing.T) (*capturingStub, string) {
	t.Helper()
	stub := newCapturingStub(t)
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() string {\n\treturn \"a\"\n}\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package b\n\nfunc B() string {\n\treturn \"b\"\n}\n")
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook:\n  - empty\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "chore(cli): add fixture")
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() string {\n\treturn \"A\"\n}\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package b\n\nfunc B() string {\n\treturn \"B\"\n}\n")
	return stub, dir
}

// TestCommitCmd_StagedBoundary_EmptyIndexMatchesDevelop is the parity check: with
// nothing staged, this branch must commit the same content, exit the same way,
// print the same envelope, and send the same prompts as develop.
func TestCommitCmd_StagedBoundary_EmptyIndexMatchesDevelop(t *testing.T) {
	develop := developBinary(t)
	args := func(stub *capturingStub) []string {
		return []string{"commit", "--api-key", "test-key", "--base-url", stub.server.URL,
			"--model", "test-model", "-o", "json"}
	}

	developStub, developDir := emptyIndexFixture(t)
	developOut, developErr, developCode := runBinary(t, develop, developDir, args(developStub)...)

	branchStub, branchDir := emptyIndexFixture(t)
	branchOut, branchErr, branchCode := runBinary(t, agentBin, branchDir, args(branchStub)...)

	if developCode != branchCode {
		t.Fatalf("exit codes differ: develop %d, branch %d\n%s\n%s", developCode, branchCode, developErr, branchErr)
	}
	if developSubjects, branchSubjects := commitSubjects(t, developDir), commitSubjects(t, branchDir); strings.Join(developSubjects, "|") != strings.Join(branchSubjects, "|") {
		t.Fatalf("commit subjects differ:\ndevelop %v\nbranch  %v", developSubjects, branchSubjects)
	}

	normalize := func(raw string) string {
		var envelope map[string]any
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			t.Fatalf("stdout is not the JSON envelope: %v\n%s", err, raw)
		}
		commits, _ := envelope["commits"].([]any)
		for _, entry := range commits {
			commit, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if _, ok := commit["sha"]; ok {
				commit["sha"] = "X"
			}
		}
		if _, ok := envelope["final_sha"]; ok {
			envelope["final_sha"] = "X"
		}
		out, _ := json.Marshal(envelope)
		return string(out)
	}
	if normalize(developOut) != normalize(branchOut) {
		t.Fatalf("the JSON envelope differs:\ndevelop %s\nbranch  %s", normalize(developOut), normalize(branchOut))
	}

	// The prompts must match too, so a comparison of commits is not the only
	// evidence: the same run has to ask the model the same questions.
	developBodies, branchBodies := developStub.captured(), branchStub.captured()
	if len(developBodies) != len(branchBodies) {
		t.Fatalf("the runs sent %d and %d requests", len(developBodies), len(branchBodies))
	}
	for i := range developBodies {
		left := strings.ReplaceAll(developBodies[i], developDir, "")
		right := strings.ReplaceAll(branchBodies[i], branchDir, "")
		if left != right {
			t.Fatalf("request %d differs:\ndevelop %s\nbranch  %s", i+1, left, right)
		}
	}
	t.Logf("empty index: %d identical request(s), identical subjects, identical envelope, exit %d", len(developBodies), branchCode)
}

// TestCommitCmd_StagedBoundary_DryRunPreviewsAndRestores proves the preview shows
// the commits a real run would make, leaves the index as it found it, and creates
// nothing.
func TestCommitCmd_StagedBoundary_DryRunPreviewsAndRestores(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir, patch := stagedBoundaryRepo(t)
	stagedBoundaryChange(t, dir)
	applyPatch(t, dir, patch)

	before := gitOutput(t, dir, "diff", "--cached")
	commitsBefore := len(commitSubjects(t, dir))

	stdout, stderr, code := gitAgentSeparated(t, dir, "commit",
		"--api-key", "test-key", "--base-url", server.URL, "--model", "test-model",
		"--dry-run", "-o", "json")
	if code != 0 {
		t.Fatalf("dry run exited %d:\n%s", code, stderr)
	}
	var preview struct {
		Commits []struct {
			Title string   `json:"title"`
			Files []string `json:"files"`
		} `json:"commits"`
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatalf("dry run did not print the envelope: %v\n%s", err, stdout)
	}
	if !preview.DryRun || len(preview.Commits) != 2 {
		t.Fatalf("the preview must report the two commits a real run makes and mark itself a dry run, got %+v", preview)
	}
	if got := preview.Commits[0].Files; len(got) != 1 || got[0] != "app.go" {
		t.Fatalf("the first previewed commit must be the staged content, got %v", got)
	}
	if after := gitOutput(t, dir, "diff", "--cached"); after != before {
		t.Fatalf("the dry run must give the index back unchanged:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := len(commitSubjects(t, dir)); got != commitsBefore {
		t.Fatalf("a dry run must create nothing: %d commits before, %d after", commitsBefore, got)
	}

	// The real run then makes exactly what the preview promised.
	runCommit(t, dir, server, 0)
	subjects := commitSubjects(t, dir)
	if len(subjects) != commitsBefore+2 {
		t.Fatalf("the real run must make the two previewed commits, got %d new: %v", len(subjects)-commitsBefore, subjects)
	}
	if status := gitOutput(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("every change must be committed:\n%s", status)
	}
}

// planRespectingStub answers the planner the way the prompt asks: the staged files
// form the first group and the unstaged files the next, and each message names the
// file its own diff covers. It exists so a comparison between two builds measures
// the flow rather than a stub that groups everything into one commit.
type planRespectingStub struct {
	server *httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newPlanRespectingStub(t *testing.T) *planRespectingStub {
	t.Helper()
	stub := &planRespectingStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		stub.mu.Lock()
		stub.bodies = append(stub.bodies, body)
		stub.mu.Unlock()

		var content string
		if strings.Contains(body, "meaningful atomic commits") {
			staged, unstaged := promptSections(body)
			var groups []string
			for _, files := range [][]string{staged, unstaged} {
				if len(files) == 0 {
					continue
				}
				quoted, _ := json.Marshal(files)
				groups = append(groups, fmt.Sprintf(`{"files":%s,"title":"feat(cli): group","bullets":["Group the files"],"explanation":"Groups the files."}`, quoted))
			}
			content = fmt.Sprintf(`{"groups":[%s]}`, strings.Join(groups, ","))
		} else {
			content = fmt.Sprintf(`{"title":"feat(cli): change %s","bullets":["Change it"],"explanation":"Changes it."}`, diffFile(body))
		}
		payload, _ := json.Marshal(content)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"x","object":"chat.completion","created":0,"model":"test-model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%s}}]}`, payload)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// promptSections reads the staged and unstaged file lists the prompt printed. The
// request body is JSON, so the prompt's newlines arrive escaped.
func promptSections(body string) (staged, unstaged []string) {
	mode := ""
	for _, line := range strings.Split(body, `\n`) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Staged files"):
			mode = "staged"
			continue
		case strings.HasPrefix(trimmed, "Unstaged files"):
			mode = "unstaged"
			continue
		case trimmed == "":
			mode = ""
			continue
		}
		if !strings.Contains(trimmed, ".") {
			continue
		}
		switch mode {
		case "staged":
			staged = append(staged, trimmed)
		case "unstaged":
			unstaged = append(unstaged, trimmed)
		}
	}
	return staged, unstaged
}

// diffFile names the file a diff describes, which is what makes two runs' commit
// messages comparable.
func diffFile(body string) string {
	match := regexp.MustCompile(`\+\+\+ b/([A-Za-z0-9_./-]+)`).FindStringSubmatch(body)
	if len(match) < 2 {
		return "unknown"
	}
	return match[1]
}

// TestCommitCmd_StagedBoundary_WholeFileStagedMatchesDevelop is the case that
// earlier looked like it always gains a commit: the index holds a complete file
// change and other files are loose. With a planner that groups what the prompt
// asked, both builds must make the same commits.
func TestCommitCmd_StagedBoundary_WholeFileStagedMatchesDevelop(t *testing.T) {
	develop := developBinary(t)

	fixture := func(t *testing.T) (*planRespectingStub, string) {
		t.Helper()
		stub := newPlanRespectingStub(t)
		dir := newGitRepo(t)
		writeFile(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() string {\n\treturn \"a\"\n}\n")
		writeFile(t, filepath.Join(dir, "b.go"), "package b\n\nfunc B() string {\n\treturn \"b\"\n}\n")
		writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
			"scopes:\n  - name: cli\n    description: CLI changes\nhook:\n  - empty\n")
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-m", "chore(cli): add fixture")
		writeFile(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() string {\n\treturn \"A\"\n}\n")
		writeFile(t, filepath.Join(dir, "b.go"), "package b\n\nfunc B() string {\n\treturn \"B\"\n}\n")
		runGit(t, dir, "add", "a.go") // the whole of a.go is staged; b.go is loose
		return stub, dir
	}

	args := func(stub *planRespectingStub) []string {
		return []string{"commit", "--api-key", "test-key", "--base-url", stub.server.URL,
			"--model", "test-model", "-o", "json"}
	}

	developStub, developDir := fixture(t)
	developOut, developErr, developCode := runBinary(t, develop, developDir, args(developStub)...)

	branchStub, branchDir := fixture(t)
	branchOut, branchErr, branchCode := runBinary(t, agentBin, branchDir, args(branchStub)...)

	if developCode != branchCode {
		t.Fatalf("exit codes differ: develop %d, branch %d\n%s\n%s", developCode, branchCode, developErr, branchErr)
	}
	developSubjects, branchSubjects := commitSubjects(t, developDir), commitSubjects(t, branchDir)
	if strings.Join(developSubjects, "|") != strings.Join(branchSubjects, "|") {
		t.Fatalf("commit subjects differ:\ndevelop %v\nbranch  %v", developSubjects, branchSubjects)
	}
	normalize := func(raw string) string {
		var envelope map[string]any
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			t.Fatalf("stdout is not the JSON envelope: %v\n%s", err, raw)
		}
		if commits, ok := envelope["commits"].([]any); ok {
			for _, entry := range commits {
				if commit, ok := entry.(map[string]any); ok {
					if _, ok := commit["sha"]; ok {
						commit["sha"] = "X"
					}
				}
			}
		}
		if _, ok := envelope["final_sha"]; ok {
			envelope["final_sha"] = "X"
		}
		out, _ := json.Marshal(envelope)
		return string(out)
	}
	if normalize(developOut) != normalize(branchOut) {
		t.Fatalf("the JSON envelope differs:\ndevelop %s\nbranch  %s", normalize(developOut), normalize(branchOut))
	}
	t.Logf("whole-file staged: develop and branch both made %d commit(s): %v", len(developSubjects), developSubjects)
	t.Logf("requests: develop %d, branch %d", len(developStub.bodies), len(branchStub.bodies))
}
