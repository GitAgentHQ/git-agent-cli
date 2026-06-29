package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCommitCmd_NoAPIKey_Returns1(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "commit", "--dry-run")
	if code != 1 {
		t.Fatalf("expected exit 1 with no API key, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "API key") {
		t.Errorf("expected 'API key' in output, got: %s", out)
	}
}

func TestCommitCmd_NoChanges_Returns1(t *testing.T) {
	dir := newGitRepo(t)
	apiKey := "test-key-does-not-matter"
	// Point to a valid but non-functional endpoint — we want to reach the
	// "no staged changes" check before the LLM is called.
	out, code := gitAgent(t, dir, "commit", "--dry-run",
		"--api-key", apiKey,
		"--base-url", "http://127.0.0.1:19999/v1",
	)
	if code != 1 {
		t.Fatalf("expected exit 1 for no changes, got %d\noutput: %s", code, out)
	}
}

func TestCommitCmd_AllFlagRemoved(t *testing.T) {
	_, code := gitAgent(t, t.TempDir(), "commit", "--all")
	if code == 0 {
		t.Fatal("expected non-zero exit for removed --all flag, got 0")
	}
}

func TestCommitCmd_DryRunFlag_Accepted(t *testing.T) {
	_, code := gitAgent(t, t.TempDir(), "commit", "--help")
	if code != 0 {
		t.Fatalf("git-agent commit --help: exit code %d", code)
	}
}

func TestCommitCmd_IntentFlag_Accepted(t *testing.T) {
	_, code := gitAgent(t, t.TempDir(), "commit", "--help")
	if code != 0 {
		t.Fatalf("git-agent commit --help: exit code %d", code)
	}
}

func TestAddCmd_Removed(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "add", "somefile.txt")
	if code == 0 {
		t.Fatal("expected non-zero exit for removed 'add' command, got 0")
	}
}

func TestCommitCmd_TrailerFlag_Accepted(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "commit", "--help")
	if code != 0 {
		t.Fatalf("git-agent commit --help: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "--trailer") {
		t.Errorf("expected --trailer flag in help output, got: %s", out)
	}
}

func TestCommitCmd_InvalidTrailerFormat_Returns1(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "commit", "--dry-run",
		"--api-key", "test-key",
		"--base-url", "http://127.0.0.1:19999/v1",
		"--trailer", "badformat",
	)
	if code != 1 {
		t.Fatalf("expected exit 1 for invalid trailer format, got %d\noutput: %s", code, out)
	}
}

func TestCommitCmd_NoStageFlag_Accepted(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "commit", "--help")
	if code != 0 {
		t.Fatalf("git-agent commit --help: exit code %d", code)
	}
	if !strings.Contains(out, "--no-stage") {
		t.Errorf("expected --no-stage flag in help output, got: %s", out)
	}
}

func TestCommitCmd_AmendFlag_Accepted(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "commit", "--help")
	if code != 0 {
		t.Fatalf("git-agent commit --help: exit code %d", code)
	}
	if !strings.Contains(out, "--amend") {
		t.Errorf("expected --amend flag in help output, got: %s", out)
	}
}

// TestCommitCmd_SIGINTCancels covers REQ-002: SIGINT during an in-flight LLM
// call cancels the request within one second and leaves the user's pre-staged
// index untouched. The fake server hijacks the connection and never replies,
// so the only way the subprocess can exit is via the signal handler.
func TestCommitCmd_SIGINTCancels(t *testing.T) {
	server, hit := newStallServer(t)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package b\n")
	writeFile(t, filepath.Join(dir, "c.go"), "package c\n")

	gitInRepo := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitInRepo("add", "a.go", "b.go")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
		"--no-stage",
	)
	c.Dir = dir
	// Minimal env: PATH for `git`, isolated XDG so the user's ~/.config is
	// never read. Inheriting os.Environ() risks pulling in CLIPROXYAPI_TOKEN
	// or other shell exports that change credential resolution.
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + t.TempDir(),
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout

	if err := c.Start(); err != nil {
		t.Fatalf("start git-agent: %v", err)
	}

	// Wait until the subprocess has issued its first LLM request — that
	// proves Go startup, cobra wiring, and signal.NotifyContext are all in
	// place. Gating on the server-side hit instead of a wall-clock sleep
	// removes the startup-race flakiness on slow machines.
	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		_ = c.Process.Kill()
		t.Fatalf("subprocess did not hit stall server within 5s\nstderr: %s", stderr.String())
	}

	signalSent := time.Now()
	if err := c.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(2 * time.Second):
		_ = c.Process.Kill()
		t.Fatalf("subprocess did not exit within 2s of SIGINT\nstderr: %s", stderr.String())
	}

	elapsed := time.Since(signalSent)
	if elapsed > time.Second {
		t.Errorf("subprocess took %s to exit after SIGINT, want ≤ 1s", elapsed)
	}

	exitCode := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("unexpected wait error: %v", waitErr)
		}
	}
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit after SIGINT, got 0\nstderr: %s\nstdout: %s", stderr.String(), stdout.String())
	}

	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "cancelled") {
		t.Errorf("stderr missing \"cancelled\", got: %q", stderrStr)
	}
	if strings.Contains(stderrStr, "panic") {
		t.Errorf("stderr contains panic stack trace: %s", stderrStr)
	}
	if strings.Contains(stderrStr, "goroutine") {
		t.Errorf("stderr contains goroutine dump: %s", stderrStr)
	}

	// The recovery path must leave the index in its pre-invocation state:
	// a.go and b.go staged, c.go unstaged.
	diffCmd := exec.Command("git", "diff", "--staged", "--name-only")
	diffCmd.Dir = dir
	out, err := diffCmd.Output()
	if err != nil {
		t.Fatalf("git diff --staged --name-only: %v", err)
	}
	staged := strings.Split(strings.TrimSpace(string(out)), "\n")
	wantStaged := []string{"a.go", "b.go"}
	if len(staged) != len(wantStaged) || staged[0] != wantStaged[0] || staged[1] != wantStaged[1] {
		t.Errorf("staged files after SIGINT = %v, want %v", staged, wantStaged)
	}
}

// TestCommitCmd_SmallDiffRegression locks in REQ-009 plus the hotfix non-TTY
// discipline. The subprocess inherits non-TTY stderr (os/exec pipes are
// not terminals), so the cmd layer suppresses phase output and heartbeats
// entirely. Result: zero always-on phase lines, zero heartbeats.
func TestCommitCmd_SmallDiffRegression(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)

	// Project config short-circuits auto-scope so the LLM is hit exactly once
	// (for message generation). Without scopes, ScopeService would issue a
	// scope-generation call first, doubling the LLM round-trips and the
	// always-on line count.
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")

	// 200-byte fixture: one staged file whose diff payload is small enough
	// that the byte cap never trips and no truncation phase line fires.
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))

	gitInRepo := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitInRepo("add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
		"--no-stage",
		"--no-attribution",
	)
	c.Dir = dir
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + t.TempDir(),
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout

	if err := c.Run(); err != nil {
		t.Fatalf("git-agent commit failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	stderrStr := stderr.String()
	stdoutStr := stdout.String()

	// Heartbeat must be silent on the small-diff path — and on every non-TTY
	// path generally, because the cmd layer suppresses the heartbeat writer.
	heartbeatRe := regexp.MustCompile(`(?m)^still waiting on LLM`)
	if matches := heartbeatRe.FindAllString(stderrStr, -1); len(matches) > 0 {
		t.Errorf("expected zero heartbeat lines, got %d:\n%s", len(matches), stderrStr)
	}

	// Non-TTY subprocess stderr: the hotfix routes always-on phase output to
	// io.Discard equivalent (nil writer), so the e2e subprocess sees an
	// empty phase stream regardless of how many groups the planner produces.
	phaseRe := regexp.MustCompile(`(?m)^(Planning commits\.\.\.|Planning commits: done \(\d+ commits?\)\.|Drafting message: \d+/\d+.*)$`)
	phaseLines := phaseRe.FindAllString(stderrStr, -1)
	if len(phaseLines) != 0 {
		t.Errorf("expected zero always-on phase lines on non-TTY stderr, got %d:\n%v\nfull stderr:\n%s",
			len(phaseLines), phaseLines, stderrStr)
	}

	// Sanity: the committed result must reach stdout (commit hash + explanation).
	if !strings.Contains(stdoutStr, "Adds one line of content for the regression fixture.") {
		t.Errorf("stdout missing commit explanation:\n%s", stdoutStr)
	}
	// `git commit` echoes the abbreviated hash inside brackets like
	// "[master (root-commit) <hash>] feat(cli): add a line".
	hashRe := regexp.MustCompile(`\[[^\]]*\b[0-9a-f]{7,}\b[^\]]*\]`)
	if !hashRe.MatchString(stdoutStr) {
		t.Errorf("stdout missing commit hash line:\n%s", stdoutStr)
	}
}

// TestCommitCmd_JSONOutput locks the agent-facing `commit -o json` contract:
// stdout is a structured envelope carrying a first-class SHA, the per-commit
// message/files, the hook outcome, and the committed_count / final_sha aggregate.
func TestCommitCmd_JSONOutput(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))

	gitInRepo := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitInRepo("add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
		"--no-stage",
		"--no-attribution",
		"-o", "json",
	)
	c.Dir = dir
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + t.TempDir(),
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		t.Fatalf("git-agent commit -o json failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	var res struct {
		DryRun         bool   `json:"dry_run"`
		CommittedCount int    `json:"committed_count"`
		FinalSHA       string `json:"final_sha"`
		Commits        []struct {
			Title       string   `json:"title"`
			Message     string   `json:"message"`
			Files       []string `json:"files"`
			SHA         string   `json:"sha"`
			HookOutcome string   `json:"hook_outcome"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("commit -o json stdout is not a JSON object: %v\n%s", err, stdout.String())
	}
	if res.DryRun {
		t.Errorf("dry_run = true, want false")
	}
	if res.CommittedCount != 1 {
		t.Errorf("committed_count = %d, want 1", res.CommittedCount)
	}
	if len(res.Commits) != 1 {
		t.Fatalf("commits len = %d, want 1\n%s", len(res.Commits), stdout.String())
	}
	cm := res.Commits[0]
	if cm.SHA == "" {
		t.Error("commit sha is empty")
	}
	if res.FinalSHA != cm.SHA {
		t.Errorf("final_sha %q != last commit sha %q", res.FinalSHA, cm.SHA)
	}
	if cm.HookOutcome != "skipped" {
		t.Errorf("hook_outcome = %q, want skipped for an empty hook", cm.HookOutcome)
	}
	if cm.Title == "" || cm.Message == "" || len(cm.Files) == 0 {
		t.Errorf("commit object missing title/message/files: %+v", cm)
	}
}

// TestCommitCmd_JSONDryRun locks the dry-run shape of `commit -o json`: the plan
// is reported with dry_run=true, committed_count 0, and empty per-commit SHAs.
func TestCommitCmd_JSONDryRun(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	if out, err := exec.Command("git", "-C", dir, "add", "readme.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	cmd := exec.Command(agentBin, "commit",
		"--api-key", "test-key", "--base-url", server.URL, "--model", "test-model",
		"--no-stage", "--no-attribution", "--dry-run", "-o", "json",
	)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "XDG_CONFIG_HOME=" + t.TempDir()}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("commit --dry-run -o json: %v\nstderr: %s", err, stderr.String())
	}

	var res struct {
		DryRun         bool `json:"dry_run"`
		CommittedCount int  `json:"committed_count"`
		Commits        []struct {
			SHA string `json:"sha"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("dry-run JSON parse: %v\n%s", err, stdout.String())
	}
	if !res.DryRun {
		t.Errorf("dry_run = false, want true")
	}
	if res.CommittedCount != 0 {
		t.Errorf("committed_count = %d, want 0 on dry-run", res.CommittedCount)
	}
	for i, cm := range res.Commits {
		if cm.SHA != "" {
			t.Errorf("commit[%d].sha = %q, want empty on dry-run", i, cm.SHA)
		}
	}
}

// TestCommitCmd_NoReasoningEffortSent locks the contract that the CLI never
// asks the model to think: even when pointed at an o-series model name
// (formerly the only branch that set reasoning_effort=low), the outbound
// chat-completion request body must carry no reasoning_effort field. All
// models run with temperature=0 and no chain-of-thought.
func TestCommitCmd_NoReasoningEffortSent(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "id": "chatcmpl-noreason",
  "object": "chat.completion",
  "created": 0,
  "model": "o3",
  "choices": [
    {"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": "{\"title\":\"feat(cli): add a line\",\"bullets\":[\"Add a new line to readme\"],\"explanation\":\"No-reasoning fixture.\"}"}}
  ]
}`))
	}))
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))

	gitInRepo := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitInRepo("add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "o3", // formerly triggered reasoning_effort=low
		"--no-stage",
		"--no-attribution",
	)
	c.Dir = dir
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + t.TempDir(),
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		t.Fatalf("git-agent commit failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	if len(capturedBody) == 0 {
		t.Fatalf("LLM server was never hit; no request body captured")
	}
	if bytes.Contains(bytes.ToLower(capturedBody), []byte("reasoning_effort")) {
		t.Errorf("request body must not contain reasoning_effort, but it does:\n%s", capturedBody)
	}
	// temperature=0 is omitted by the encoder (omitempty); its absence is the
	// contract. A present, non-zero temperature would violate "no think mode".
	if bytes.Contains(capturedBody, []byte(`"temperature"`)) {
		t.Errorf("request body must not pin a non-zero temperature:\n%s", capturedBody)
	}
}

func TestCommitCmd_AmendAndNoStage_MutuallyExclusive(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "commit", "--amend", "--no-stage",
		"--api-key", "test-key",
		"--base-url", "http://127.0.0.1:19999/v1",
	)
	if code == 0 {
		t.Fatalf("expected non-zero exit for --amend + --no-stage, got 0\noutput: %s", out)
	}
}

// commitOneFile runs a real single-file commit through the fast LLM server. A
// single staged file skips the planning round-trip, so the canned message
// response suffices. configYML seeds .git-agent/config.yml (a scope
// short-circuits auto-scope so the LLM is hit exactly once). It fails the test
// if the commit does not succeed.
func commitOneFile(t *testing.T, dir, serverURL, configYML string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"), configYML)
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	runGit(t, dir, "add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", serverURL,
		"--model", "test-model",
		"--no-stage",
		"--no-attribution",
	)
	c.Dir = dir
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + t.TempDir(),
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		t.Fatalf("git-agent commit failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}
}

// TestCommitCmd_BootstrapsGraph asserts commit is the git-first graph-generation
// path: a commit in a repo with no graph bootstraps .git-agent/graph.db and
// folds the new commit into the co-change index.
func TestCommitCmd_BootstrapsGraph(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()
	dir := newGitRepo(t)

	graphDB := filepath.Join(dir, ".git-agent", "graph.db")
	if _, err := os.Stat(graphDB); err == nil {
		t.Fatal("precondition: graph.db should not exist before the first commit")
	}

	commitOneFile(t, dir, server.URL, "scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")

	if _, err := os.Stat(graphDB); err != nil {
		t.Fatalf("expected commit to bootstrap %s, but it is absent: %v", graphDB, err)
	}

	// The new commit must be reflected in the co-change index.
	out, code := gitAgent(t, dir, "graph", "status", "-o", "json")
	if code != 0 {
		t.Fatalf("graph status exit %d: %s", code, out)
	}
	if !strings.Contains(out, `"commit_count"`) || strings.Contains(out, `"commit_count": 0`) {
		t.Errorf("expected commit_count > 0 after commit, got: %s", out)
	}
}

// TestCommitCmd_GraphAutobuildOptOut asserts graph_autobuild=false keeps the
// graph untouched: the commit still succeeds and no graph.db is created.
func TestCommitCmd_GraphAutobuildOptOut(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()
	dir := newGitRepo(t)

	commitOneFile(t, dir, server.URL,
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\ngraph_autobuild: false\n")

	if _, err := os.Stat(filepath.Join(dir, ".git-agent", "graph.db")); !os.IsNotExist(err) {
		t.Errorf("graph_autobuild=false must not create graph.db; stat err = %v", err)
	}
}
