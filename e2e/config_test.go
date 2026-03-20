package e2e_test

import (
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
	writeFile(t, filepath.Join(dir, ".git-agent", "project.yml"), "scopes:\n  - api\n  - cli\n")

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
