package e2e_test

import (
	"bytes"
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
