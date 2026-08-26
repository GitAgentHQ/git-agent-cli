package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitLogBody returns the full body of the most recent commit in dir.
func gitLogBody(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log -1: %v", err)
	}
	return string(out)
}

// TestCommitCmd_CoAuthorTrailer locks in the `git-agent commit --co-author`
// contract: each flag appends a Co-Authored-By trailer to the committed
// message, and the flag is repeatable. Without the flag the commit carries no
// explicit Co-Authored-By trailer.
func TestCommitCmd_CoAuthorTrailer(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	runGit(t, dir, "add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
		"--no-stage",
		"--co-author", "Alice <alice@example.com>",
		"--co-author", "Bob <bob@example.com>",
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
		t.Fatalf("git-agent commit --co-author failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	msg := gitLogBody(t, dir)
	for _, want := range []string{
		"Co-Authored-By: Alice <alice@example.com>",
		"Co-Authored-By: Bob <bob@example.com>",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("commit message missing %q, got:\n%s", want, msg)
		}
	}
}

// TestCommitCmd_CoAuthorNameOnly locks in the regression fix: git accepts
// name-only Co-Authored-By trailers (trailers are free-form text), so a
// --co-author value without an email must pass the conventional hook instead
// of being rejected until retries are exhausted.
func TestCommitCmd_CoAuthorNameOnly(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook:\n  - conventional\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	runGit(t, dir, "add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
		"--no-stage",
		"--co-author", "OX Alpha",
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
		t.Fatalf("git-agent commit --co-author (name only) failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	msg := gitLogBody(t, dir)
	if !strings.Contains(msg, "Co-Authored-By: OX Alpha") {
		t.Errorf("commit message missing name-only Co-Authored-By trailer, got:\n%s", msg)
	}
}

// TestAutonomousRoot_CoAuthorTrailer locks in `--co-author` on the bare root
// command: the autonomous workflow must pass the flag through to the commit
// pipeline exactly like `git-agent commit --co-author`. The fixture seeds a
// complete .gitignore (all mandatory rules) and a scoped project config in an
// initial commit, so the only working-tree change the root flow sees is
// readme.txt — exactly one LLM call (message generation), no auto-init noise.
func TestAutonomousRoot_CoAuthorTrailer(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".gitignore"),
		".git-agent/graph.db\n*.db-shm\n*.db-wal\n*.db-journal\n.git-agent/config.local.yml\n")
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")
	runGit(t, dir, "add", ".gitignore", ".git-agent/config.yml")
	runGit(t, dir, "commit", "-m", "chore: seed project config")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))

	c := exec.Command(agentBin,
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
		"--co-author", "Carol <carol@example.com>",
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
		t.Fatalf("git-agent --co-author failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	msg := gitLogBody(t, dir)
	if !strings.Contains(msg, "Co-Authored-By: Carol <carol@example.com>") {
		t.Errorf("commit message missing Co-Authored-By trailer, got:\n%s", msg)
	}
}

// TestDirectRun_InferenceModelDoesNotAddCoAuthorTrailer ensures that when a user
// runs git-agent directly with an inference model (e.g. gemini-3.1-flash-lite),
// no Co-Authored-By trailer is added for the inference model.
func TestDirectRun_InferenceModelDoesNotAddCoAuthorTrailer(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	runGit(t, dir, "add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "gemini-3.1-flash-lite",
		"--no-stage",
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

	msg := gitLogBody(t, dir)
	if strings.Contains(msg, "Co-Authored-By: Gemini") || strings.Contains(msg, "google.com") {
		t.Errorf("expected no model Co-Authored-By trailer from inference model, got:\n%s", msg)
	}
}

// TestAgentSession_SessionModelAddsCoAuthorTrailer ensures that when git-agent
// runs inside an active agent session (PI_MODEL is set), a Co-Authored-By trailer
// is inferred from the session model.
func TestAgentSession_SessionModelAddsCoAuthorTrailer(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"),
		"scopes:\n  - name: cli\n    description: CLI changes\nhook: empty\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	runGit(t, dir, "add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--api-key", "test-key",
		"--base-url", server.URL,
		"--model", "test-model",
		"--no-stage",
	)
	c.Dir = dir
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + t.TempDir(),
		"PI_MODEL=gemini-3.1-flash-lite",
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		t.Fatalf("git-agent commit failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	msg := gitLogBody(t, dir)
	if !strings.Contains(msg, "Co-Authored-By: Gemini 3.1 Flash Lite <noreply@google.com>") {
		t.Errorf("expected session model Co-Authored-By trailer, got:\n%s", msg)
	}
}

// TestAgentSession_OxAlphaUsesNameOnlyCoAuthor ensures Ox Alpha is attributed
// without a synthetic email address and satisfies require_model_co_author.
func TestAgentSession_OxAlphaUsesNameOnlyCoAuthor(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "git-agent", "config.yml"),
		fmt.Sprintf("api_key: test-key\nbase_url: %s/v1\nmodel: test-model\nrequire_model_co_author: true\nhook: empty\n", server.URL))

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	runGit(t, dir, "add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--intent", "update readme contents",
		"--no-stage",
	)
	c.Dir = dir
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"PI_MODEL=openrouter/stealth/ox-alpha",
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		t.Fatalf("git-agent commit failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	msg := gitLogBody(t, dir)
	if !strings.Contains(msg, "Co-Authored-By: Ox Alpha") {
		t.Errorf("expected name-only Ox Alpha Co-Authored-By trailer, got:\n%s", msg)
	}
	if strings.Contains(msg, "models.git-agent.dev") {
		t.Errorf("expected Ox Alpha trailer without fallback domain, got:\n%s", msg)
	}
}

// TestAgentSession_UnmappedSessionModelAddsCoAuthorTrailer ensures a session
// model that maps to no known provider (other than Ox Alpha) is still
// attributed under the fallback domain and satisfies require_model_co_author.
func TestAgentSession_UnmappedSessionModelAddsCoAuthorTrailer(t *testing.T) {
	server := newFastLLMServer(t, 0)
	defer server.Close()

	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "git-agent", "config.yml"),
		fmt.Sprintf("api_key: test-key\nbase_url: %s/v1\nmodel: test-model\nrequire_model_co_author: true\nhook: empty\n", server.URL))

	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, "readme.txt"), strings.Repeat("a", 200))
	runGit(t, dir, "add", "readme.txt")

	c := exec.Command(agentBin, "commit",
		"--intent", "update readme contents",
		"--no-stage",
	)
	c.Dir = dir
	c.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"PI_MODEL=openrouter/stealth/custom-model",
	}
	var stderr, stdout bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		t.Fatalf("git-agent commit failed: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	msg := gitLogBody(t, dir)
	if !strings.Contains(msg, "Co-Authored-By: Custom Model <noreply@models.git-agent.dev>") {
		t.Errorf("expected fallback-domain session model Co-Authored-By trailer, got:\n%s", msg)
	}
}
