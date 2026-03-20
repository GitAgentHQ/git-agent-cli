package hook_test

import (
	"context"
	"strings"
	"testing"

	domainHook "github.com/fradser/git-agent/domain/hook"
	domainProject "github.com/fradser/git-agent/domain/project"
	infraHook "github.com/fradser/git-agent/infrastructure/hook"
)

func validCommitMessage() string {
	return "feat: add user authentication\n\n- add login endpoint\n- add jwt token generation\n\nThis introduces basic authentication support.\n\nCo-Authored-By: Bot <bot@example.com>"
}

func compositeInput(msg string) domainHook.HookInput {
	return domainHook.HookInput{
		CommitMessage: msg,
		StagedFiles:   []string{"auth.go"},
		Config:        domainProject.Config{},
	}
}

func TestCompositeExecutor_ValidMessage_NoShellHook(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), "/nonexistent/hook", compositeInput(validCommitMessage()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestCompositeExecutor_InvalidMessage_Blocked(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), "/nonexistent/hook", compositeInput("bad commit message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for invalid message")
	}
	if result.Stderr == "" {
		t.Error("expected non-empty stderr with error details")
	}
}

func TestCompositeExecutor_InvalidMessage_StderrContainsErrors(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()
	input := compositeInput("bad commit: no body here")

	result, err := exec.Execute(context.Background(), "/nonexistent/hook", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stderr, "error:") {
		t.Errorf("expected 'error:' prefix in stderr, got: %s", result.Stderr)
	}
}

func TestCompositeExecutor_ValidMessage_ShellHookBlocks(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\necho 'custom check failed' >&2\nexit 1\n", 0o755)
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), script, compositeInput(validCommitMessage()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected shell hook to block")
	}
	if !strings.Contains(result.Stderr, "custom check failed") {
		t.Errorf("expected shell hook stderr, got: %s", result.Stderr)
	}
}

func TestCompositeExecutor_ValidMessage_ShellHookPasses(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\nexit 0\n", 0o755)
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), script, compositeInput(validCommitMessage()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestCompositeExecutor_WarningsOnly_Passes(t *testing.T) {
	// Description starts with past-tense verb — warning, not error
	msg := "feat: added user authentication\n\n- add login endpoint\n\nThis introduces authentication support."
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), "/nonexistent/hook", compositeInput(msg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0 for warnings-only message, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "warning:") {
		t.Errorf("expected 'warning:' in stderr, got: %s", result.Stderr)
	}
}
