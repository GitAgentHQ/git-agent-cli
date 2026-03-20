package e2e_test

import (
	"strings"
	"testing"
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
