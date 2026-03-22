package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigShowCmd_Runs(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "show")
	if code != 0 {
		t.Fatalf("git-agent config show: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "model:") {
		t.Errorf("expected 'model:' in output, got: %s", out)
	}
}

func TestConfigScopesCmd_NoConfig(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "scopes")
	if code != 0 {
		t.Fatalf("git-agent config scopes: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "no project config") {
		t.Errorf("expected 'no project config' in output, got: %s", out)
	}
}

func TestConfigScopesCmd_WithConfig(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"), "scopes:\n  - api\n  - cli\n")

	out, code := gitAgent(t, dir, "config", "scopes")
	if code != 0 {
		t.Fatalf("git-agent config scopes: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "api") {
		t.Errorf("expected 'api' scope in output, got: %s", out)
	}
	if !strings.Contains(out, "cli") {
		t.Errorf("expected 'cli' scope in output, got: %s", out)
	}
}

func TestConfigScopesCmd_FallbackToProjectYml(t *testing.T) {
	dir := newGitRepo(t)
	// Write to legacy project.yml — config.yml absent.
	writeFile(t, filepath.Join(dir, ".git-agent", "project.yml"), "scopes:\n  - legacy\n")

	out, code := gitAgent(t, dir, "config", "scopes")
	if code != 0 {
		t.Fatalf("git-agent config scopes: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "legacy") {
		t.Errorf("expected 'legacy' scope from project.yml fallback, got: %s", out)
	}
}

func TestConfigSet_ProjectScope(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "set", "hook", "conventional", "--project")
	if code != 0 {
		t.Fatalf("config set hook: exit code %d\noutput: %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git-agent", "config.yml"))
	if err != nil {
		t.Fatalf("config.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "conventional") {
		t.Errorf("expected 'conventional' in config.yml, got:\n%s", data)
	}
}

func TestConfigSet_LocalScope(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "set", "no_git_agent_co_author", "true", "--local")
	if code != 0 {
		t.Fatalf("config set --local: exit code %d\noutput: %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git-agent", "config.local.yml"))
	if err != nil {
		t.Fatalf("config.local.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "true") {
		t.Errorf("expected 'true' in config.local.yml, got:\n%s", data)
	}
}

func TestConfigSet_RejectsProviderKeyInProject(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "set", "api_key", "sk-test", "--project")
	if code == 0 {
		t.Fatalf("expected non-zero exit for api_key --project, got 0\noutput: %s", out)
	}
}

func TestConfigSet_DefaultScopeForProviderKey(t *testing.T) {
	dir := newGitRepo(t)
	xdgDir := t.TempDir()
	// model defaults to --user scope
	out, code := gitAgentEnv(t, dir, []string{"XDG_CONFIG_HOME=" + xdgDir}, "config", "set", "model", "gpt-4o")
	if code != 0 {
		t.Fatalf("config set model: exit code %d\noutput: %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(xdgDir, "git-agent", "config.yml"))
	if err != nil {
		t.Fatalf("user config.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "gpt-4o") {
		t.Errorf("expected 'gpt-4o' in user config.yml, got:\n%s", data)
	}
}

func TestConfigGet_ProjectScope(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"), "hook:\n  - conventional\n")

	out, code := gitAgent(t, dir, "config", "get", "hook")
	if code != 0 {
		t.Fatalf("config get hook: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "conventional") {
		t.Errorf("expected 'conventional' in output, got: %s", out)
	}
	if !strings.Contains(out, "project") {
		t.Errorf("expected 'project' scope in output, got: %s", out)
	}
}

func TestConfigGet_LocalOverridesProject(t *testing.T) {
	dir := newGitRepo(t)
	writeFile(t, filepath.Join(dir, ".git-agent", "config.yml"), "hook:\n  - conventional\n")
	writeFile(t, filepath.Join(dir, ".git-agent", "config.local.yml"), "hook:\n  - empty\n")

	out, code := gitAgent(t, dir, "config", "get", "hook")
	if code != 0 {
		t.Fatalf("config get hook: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("expected 'empty' (local wins) in output, got: %s", out)
	}
	if !strings.Contains(out, "local") {
		t.Errorf("expected 'local' scope in output, got: %s", out)
	}
}

func TestConfigGet_NotSet(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "config", "get", "hook")
	if code != 0 {
		t.Fatalf("config get hook: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "not set") {
		t.Errorf("expected 'not set' in output, got: %s", out)
	}
}

func TestConfigGet_UnknownKey(t *testing.T) {
	dir := newGitRepo(t)
	_, code := gitAgent(t, dir, "config", "get", "nonexistent_key")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown key, got 0")
	}
}
