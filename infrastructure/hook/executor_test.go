package hook_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	domainHook "github.com/fradser/ga-cli/domain/hook"
	domainProject "github.com/fradser/ga-cli/domain/project"
	infraHook "github.com/fradser/ga-cli/infrastructure/hook"
)

func newExecutor() domainHook.HookExecutor {
	return infraHook.NewShellHookExecutor()
}

func writeTempScript(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	f, err := os.CreateTemp(os.TempDir(), "ga-hook-*.sh")
	if err != nil {
		t.Fatalf("create temp script: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp script: %v", err)
	}
	f.Close()
	if err := os.Chmod(f.Name(), mode); err != nil {
		t.Fatalf("chmod temp script: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func sampleInput() domainHook.HookInput {
	return domainHook.HookInput{
		Diff:          "diff --git a/foo.go b/foo.go",
		CommitMessage: "feat: add foo",
		Intent:        "add feature",
		StagedFiles:   []string{"foo.go", "bar.go"},
		Config:        domainProject.Config{Scopes: []string{"api", "cli"}},
	}
}

func TestExecute_HookPasses(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\nexit 0\n", 0o755)
	exec := newExecutor()

	result, err := exec.Execute(context.Background(), script, sampleInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestExecute_HookBlocks(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\necho 'blocked' >&2\nexit 1\n", 0o755)
	exec := newExecutor()

	result, err := exec.Execute(context.Background(), script, sampleInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
	if result.Stderr == "" {
		t.Error("expected non-empty Stderr")
	}
}

func TestExecute_HookDoesNotExist(t *testing.T) {
	exec := newExecutor()
	missing := filepath.Join(os.TempDir(), "ga-hook-nonexistent-xyz.sh")

	result, err := exec.Execute(context.Background(), missing, sampleInput())
	if err != nil {
		t.Fatalf("expected no error for missing hook, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for missing hook")
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0 for missing hook, got %d", result.ExitCode)
	}
}

func TestExecute_HookNotExecutable(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\nexit 0\n", 0o644)
	exec := newExecutor()

	_, err := exec.Execute(context.Background(), script, sampleInput())
	if err == nil {
		t.Error("expected error for non-executable hook")
	}
}

func TestExecute_JSONPayloadStructure(t *testing.T) {
	// Script reads stdin, parses JSON, and exits non-zero if a required field is missing.
	script := writeTempScript(t, `#!/bin/sh
input=$(cat)
for field in diff commit_message intent staged_files config; do
  echo "$input" | grep -q "\"$field\"" || { echo "missing field: $field" >&2; exit 2; }
done
exit 0
`, 0o755)

	input := sampleInput()
	exec := newExecutor()

	result, err := exec.Execute(context.Background(), script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode != 0 {
		t.Errorf("JSON payload missing required fields; stderr: %s", result.Stderr)
	}

	// Also verify the payload marshals with all expected keys.
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	for _, key := range []string{"diff", "commit_message", "intent", "staged_files", "config"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON payload missing key %q", key)
		}
	}
}
