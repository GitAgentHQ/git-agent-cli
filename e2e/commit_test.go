package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitCmd_NoAPIKey_Returns1(t *testing.T) {
	dir := newGitRepo(t)
	out, code := ga(t, dir, "commit", "--dry-run")
	if code != 1 {
		t.Fatalf("expected exit 1 with no API key, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "API key") {
		t.Errorf("expected 'API key' in output, got: %s", out)
	}
}

func TestCommitCmd_NoStagedChanges_Returns1(t *testing.T) {
	dir := newGitRepo(t)
	apiKey := "test-key-does-not-matter"
	// Point to a valid but non-functional endpoint — we want to reach the
	// "no staged changes" check before the LLM is called.
	out, code := ga(t, dir, "commit", "--dry-run",
		"--api-key", apiKey,
		"--base-url", "http://127.0.0.1:19999/v1",
	)
	if code != 1 {
		t.Fatalf("expected exit 1 for no staged changes, got %d\noutput: %s", code, out)
	}
}

func TestCommitCmd_AllFlagAccepted(t *testing.T) {
	_, code := ga(t, t.TempDir(), "commit", "--help")
	if code != 0 {
		t.Fatalf("ga commit --help: exit code %d", code)
	}
}

func TestAddCmd_RequiresPathspec(t *testing.T) {
	dir := newGitRepo(t)
	_, code := ga(t, dir, "add")
	if code == 0 {
		t.Fatal("expected non-zero exit when no pathspec provided, got 0")
	}
}

func TestAddCmd_StagesFileInGitRepo(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, "hello.txt"), "hello\n")

	_, code := ga(t, dir, "add", "hello.txt")
	if code != 0 {
		t.Fatalf("ga add hello.txt: exit code %d", code)
	}
}
