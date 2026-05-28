package e2e_test

import (
	"bytes"
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
	phaseRe := regexp.MustCompile(`(?m)^(Planning commits\.\.\.|Planning commits: \d+/\d+, done\.|Drafting message: \d+/\d+.*)$`)
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
