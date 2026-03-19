package e2e_test

import (
	"path/filepath"
	"testing"
)

func TestCommitCmd_DryRunFlagAccepted(t *testing.T) {
	dir := t.TempDir()
	_, code := ga(t, dir, "commit", "--dry-run")
	if code != 0 {
		t.Fatalf("ga commit --dry-run: exit code %d", code)
	}
}

func TestCommitCmd_AllFlagAccepted(t *testing.T) {
	dir := t.TempDir()
	_, code := ga(t, dir, "commit", "--dry-run", "--all")
	if code != 0 {
		t.Fatalf("ga commit --dry-run --all: exit code %d", code)
	}
}

func TestCommitCmd_IntentFlagAccepted(t *testing.T) {
	dir := t.TempDir()
	_, code := ga(t, dir, "commit", "--dry-run", "--intent", "fix login bug")
	if code != 0 {
		t.Fatalf("ga commit --intent: exit code %d", code)
	}
}

func TestCommitCmd_VerboseFlagAccepted(t *testing.T) {
	dir := t.TempDir()
	_, code := ga(t, dir, "commit", "--dry-run", "--verbose")
	if code != 0 {
		t.Fatalf("ga commit --verbose: exit code %d", code)
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
