package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGitignoreFlag_NoAPIKey_Fails(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--gitignore")
	if code == 0 {
		t.Fatal("expected non-zero exit when no API key configured")
	}
}

func TestInitGitignoreFlag_NotGitRepo_Fails(t *testing.T) {
	dir := t.TempDir()
	_, code := gitAgentEnv(t, dir, []string{"GIT_AGENT_API_KEY=fake"}, "init", "--gitignore")
	if code == 0 {
		t.Fatal("expected non-zero exit outside git repository")
	}
}

func TestInitGitignoreFlag_ForceFlag_Recognized(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".gitignore"), "# existing\n")

	out, _ := gitAgent(t, dir, "init", "--gitignore", "--force")
	if strings.Contains(out, "unknown flag") {
		t.Errorf("--force flag not recognized: %s", out)
	}
}

func TestInitGitignoreFlag_DoesNotBreakOtherFlags(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "init", "--hook", "empty")
	if code != 0 {
		t.Fatalf("init --hook empty still works: exit code %d", code)
	}
	projectYML := filepath.Join(dir, ".git-agent", "project.yml")
	data, err := os.ReadFile(projectYML)
	if err != nil {
		t.Fatalf("project.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "hook_type: empty") {
		t.Errorf("project.yml missing hook_type: empty, got:\n%s", data)
	}
}
