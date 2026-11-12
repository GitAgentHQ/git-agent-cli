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

// These tests compare this tree with the revision immediately before the
// staged-boundary fix. The comparison is a test rather than a note. A moving
// branch stopped representing the defect once the fix landed there, so the
// baseline is pinned and built once in a detached worktree.
const stagedBoundaryBaseline = "6c58d09" // parent of cdf55b0

var (
	baselineBinOnce sync.Once
	baselineBin     string
	baselineBinErr  error
)

// baselineBinary builds the pinned pre-fix baseline once and returns its binary
// path. It skips when the revision is not available, which is what a shallow
// checkout sees.
func baselineBinary(t *testing.T) string {
	t.Helper()
	baselineBinOnce.Do(func() {
		root, err := repoRootOf()
		if err != nil {
			baselineBinErr = fmt.Errorf("locating the repository: %w", err)
			return
		}
		if err := exec.Command("git", "-C", root, "rev-parse", "--verify", stagedBoundaryBaseline+"^{commit}").Run(); err != nil {
			baselineBinErr = fmt.Errorf("the pre-fix baseline revision is not in this checkout")
			return
		}
		dir, err := os.MkdirTemp("", "git-agent-baseline-*")
		if err != nil {
			baselineBinErr = err
			return
		}
		// The binary is written outside the checkout, so the worktree can be
		// removed again and the repository keeps no stale registration.
		binDir, err := os.MkdirTemp("", "git-agent-baseline-bin-*")
		if err != nil {
			baselineBinErr = err
			return
		}
		baselineBin = filepath.Join(binDir, "git-agent")
		if out, err := runGitIn(root, "worktree", "add", "--detach", dir, stagedBoundaryBaseline); err != nil {
			baselineBinErr = fmt.Errorf("worktree add: %v\n%s", err, out)
			return
		}
		defer func() {
			_, _ = runGitIn(root, "worktree", "remove", "--force", dir)
			_ = os.RemoveAll(dir)
		}()
		build := exec.Command("go", "build", "-o", baselineBin, ".")
		build.Dir = dir
		if out, err := build.CombinedOutput(); err != nil {
			baselineBinErr = fmt.Errorf("building pre-fix baseline: %v\n%s", err, out)
			return
		}
	})
	if baselineBinErr != nil {
		t.Skipf("cannot compare with the pre-fix baseline: %v", baselineBinErr)
	}
	return baselineBin
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

// TestCommitCmd_StagedBoundary_BaselineAbsorbsWhatTheBranchKeepsApart is the
// comparison this fix exists for: the pinned pre-fix baseline and this tree run
// against the same fixture, with one region staged and the other loose.
func TestCommitCmd_StagedBoundary_BaselineAbsorbsWhatTheBranchKeepsApart(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()
	baseline := baselineBinary(t)

	args := []string{"commit", "--api-key", "test-key", "--base-url", server.URL,
		"--model", "test-model", "--no-stage"}

	baselineDir, baselinePatch := stagedBoundaryRepo(t)
	stagedBoundaryChange(t, baselineDir)
	applyPatch(t, baselineDir, baselinePatch)
	if _, stderr, code := runBinary(t, baseline, baselineDir, args...); code != 0 {
		t.Fatalf("the baseline binary failed with %d:\n%s", code, stderr)
	}
	baselineHead := gitOutput(t, baselineDir, "show", "HEAD", "--", "app.go")
	if !strings.Contains(baselineHead, "EPSILON") {
		t.Fatalf("the fixture no longer reproduces the baseline defect:\n%s", baselineHead)
	}
	if loose := gitOutput(t, baselineDir, "diff", "--name-only"); strings.TrimSpace(loose) != "" {
		t.Fatalf("baseline left work behind, so it did not absorb it:\n%s", loose)
	}
	t.Logf("pre-fix baseline: the staged commit absorbed the unstaged region, and the working tree was left empty")

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

// llmStub records the request bodies it is asked to answer, so two runs can be
// compared on the prompts they sent as well as on what they committed. respond
// builds the assistant reply for one request body.
type llmStub struct {
	server  *httptest.Server
	mu      sync.Mutex
	bodies  []string
	respond func(body string) string
}

func newLLMStub(t *testing.T, respond func(body string) string) *llmStub {
	t.Helper()
	stub := &llmStub{respond: respond}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		stub.mu.Lock()
		stub.bodies = append(stub.bodies, body)
		stub.mu.Unlock()

		payload, _ := json.Marshal(stub.respond(body))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"x","object":"chat.completion","created":0,"model":"test-model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%s}}]}`, payload)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *llmStub) captured() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// newCapturingStub answers every request with a fixed message, except the
// planner which it answers with the files its prompt listed.
func newCapturingStub(t *testing.T) *llmStub {
	t.Helper()
	return newLLMStub(t, func(body string) string {
		if strings.Contains(body, "meaningful atomic commits") {
			return planFromPrompt(body)
		}
		return `{"title":"feat(cli): initial commit","bullets":["Change the files"],"explanation":"Changes the files."}`
	})
}

// emptyIndexFixture is a repository with two changed files and nothing staged, so
// the run takes the file-level path and asks the planner once.
func emptyIndexFixture(t *testing.T) (*llmStub, string) {
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

// TestCommitCmd_StagedBoundary_EmptyIndexMatchesBaseline is the parity check: with
// nothing staged, this branch must commit the same content, exit the same way,
// print the same envelope, and send the same prompts as baseline.
func TestCommitCmd_StagedBoundary_EmptyIndexMatchesBaseline(t *testing.T) {
	baseline := baselineBinary(t)
	args := func(stub *llmStub) []string {
		return []string{"commit", "--api-key", "test-key", "--base-url", stub.server.URL,
			"--model", "test-model", "-o", "json"}
	}

	baselineStub, baselineDir := emptyIndexFixture(t)
	baselineOut, baselineErr, baselineCode := runBinary(t, baseline, baselineDir, args(baselineStub)...)

	branchStub, branchDir := emptyIndexFixture(t)
	branchOut, branchErr, branchCode := runBinary(t, agentBin, branchDir, args(branchStub)...)

	if baselineCode != branchCode {
		t.Fatalf("exit codes differ: baseline %d, branch %d\n%s\n%s", baselineCode, branchCode, baselineErr, branchErr)
	}
	if baselineSubjects, branchSubjects := commitSubjects(t, baselineDir), commitSubjects(t, branchDir); strings.Join(baselineSubjects, "|") != strings.Join(branchSubjects, "|") {
		t.Fatalf("commit subjects differ:\nbaseline %v\nbranch  %v", baselineSubjects, branchSubjects)
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
	if normalize(baselineOut) != normalize(branchOut) {
		t.Fatalf("the JSON envelope differs:\nbaseline %s\nbranch  %s", normalize(baselineOut), normalize(branchOut))
	}

	// The prompts must match too, so a comparison of commits is not the only
	// evidence: the same run has to ask the model the same questions.
	baselineBodies, branchBodies := baselineStub.captured(), branchStub.captured()
	if len(baselineBodies) != len(branchBodies) {
		t.Fatalf("the runs sent %d and %d requests", len(baselineBodies), len(branchBodies))
	}
	for i := range baselineBodies {
		left := strings.ReplaceAll(baselineBodies[i], baselineDir, "")
		right := strings.ReplaceAll(branchBodies[i], branchDir, "")
		if left != right {
			t.Fatalf("request %d differs:\nbaseline %s\nbranch  %s", i+1, left, right)
		}
	}
	t.Logf("empty index: %d identical request(s), identical subjects, identical envelope, exit %d", len(baselineBodies), branchCode)
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

// newPlanRespectingStub answers the planner the way the prompt asks: the staged
// files form the first group and the unstaged files the next, and each message
// names the file its own diff covers. It exists so a comparison between two
// builds measures the flow rather than a stub that groups everything into one
// commit.
func newPlanRespectingStub(t *testing.T) *llmStub {
	t.Helper()
	return newLLMStub(t, func(body string) string {
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
			return fmt.Sprintf(`{"groups":[%s]}`, strings.Join(groups, ","))
		}
		return fmt.Sprintf(`{"title":"feat(cli): change %s","bullets":["Change it"],"explanation":"Changes it."}`, diffFile(body))
	})
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

// TestCommitCmd_StagedBoundary_WholeFileStagedMatchesBaseline is the case that
// earlier looked like it always gains a commit: the index holds a complete file
// change and other files are loose. With a planner that groups what the prompt
// asked, both builds must make the same commits.
func TestCommitCmd_StagedBoundary_WholeFileStagedMatchesBaseline(t *testing.T) {
	baseline := baselineBinary(t)

	fixture := func(t *testing.T) (*llmStub, string) {
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

	args := func(stub *llmStub) []string {
		return []string{"commit", "--api-key", "test-key", "--base-url", stub.server.URL,
			"--model", "test-model", "-o", "json"}
	}

	baselineStub, baselineDir := fixture(t)
	baselineOut, baselineErr, baselineCode := runBinary(t, baseline, baselineDir, args(baselineStub)...)

	branchStub, branchDir := fixture(t)
	branchOut, branchErr, branchCode := runBinary(t, agentBin, branchDir, args(branchStub)...)

	if baselineCode != branchCode {
		t.Fatalf("exit codes differ: baseline %d, branch %d\n%s\n%s", baselineCode, branchCode, baselineErr, branchErr)
	}
	baselineSubjects, branchSubjects := commitSubjects(t, baselineDir), commitSubjects(t, branchDir)
	if strings.Join(baselineSubjects, "|") != strings.Join(branchSubjects, "|") {
		t.Fatalf("commit subjects differ:\nbaseline %v\nbranch  %v", baselineSubjects, branchSubjects)
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
	if normalize(baselineOut) != normalize(branchOut) {
		t.Fatalf("the JSON envelope differs:\nbaseline %s\nbranch  %s", normalize(baselineOut), normalize(branchOut))
	}
	t.Logf("whole-file staged: baseline and this tree both made %d commit(s): %v", len(baselineSubjects), baselineSubjects)
	t.Logf("requests: baseline %d, this tree %d", len(baselineStub.bodies), len(branchStub.bodies))
}
