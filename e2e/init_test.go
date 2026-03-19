package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCmd_DefaultHook_CreatesFiles(t *testing.T) {
	dir := newGitRepo(t)
	_, code := ga(t, dir, "init")
	if code != 0 {
		t.Fatalf("ga init: exit code %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ga", "project.yml")); err != nil {
		t.Errorf(".ga/project.yml not created: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, ".ga", "hooks", "pre-commit"))
	if err != nil {
		t.Errorf(".ga/hooks/pre-commit not created: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Error("pre-commit hook is not executable")
	}
}

func TestInitCmd_ConventionalHook(t *testing.T) {
	dir := newGitRepo(t)
	_, code := ga(t, dir, "init", "--hook", "conventional")
	if code != 0 {
		t.Fatalf("ga init --hook conventional: exit code %d", code)
	}
}

func TestInitCmd_CommitMsgHook(t *testing.T) {
	dir := newGitRepo(t)
	_, code := ga(t, dir, "init", "--hook", "commit-msg")
	if code != 0 {
		t.Fatalf("ga init --hook commit-msg: exit code %d", code)
	}
}

func TestInitCmd_UnknownHookIsRejected(t *testing.T) {
	dir := t.TempDir()
	_, code := ga(t, dir, "init", "--hook", "bad-hook")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown hook, got 0")
	}
}

func TestInitCmd_ExistingConfigBlocksWithoutForce(t *testing.T) {
	dir := newGitRepo(t)
	ymlPath := filepath.Join(dir, "project.yml")
	writeFile(t, ymlPath, "existing: true\n")

	_, code := ga(t, dir, "init", "--config", ymlPath)
	if code == 0 {
		t.Fatal("expected non-zero exit when config exists without --force, got 0")
	}
}

func TestInitCmd_ForceOverwritesExistingConfig(t *testing.T) {
	dir := newGitRepo(t)
	ymlPath := filepath.Join(dir, "project.yml")
	writeFile(t, ymlPath, "existing: true\n")

	_, code := ga(t, dir, "init", "--config", ymlPath, "--force")
	if code != 0 {
		t.Fatalf("ga init --force should succeed, exit code %d", code)
	}
}

func TestInitCmd_MaxCommitsFlag(t *testing.T) {
	dir := newGitRepo(t)
	_, code := ga(t, dir, "init", "--max-commits", "50")
	if code != 0 {
		t.Fatalf("ga init --max-commits 50: exit code %d", code)
	}
}
