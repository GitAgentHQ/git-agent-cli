package hook_test

import (
	"context"
	"strings"
	"testing"

	domainHook "github.com/gitagenthq/git-agent/domain/hook"
	domainProject "github.com/gitagenthq/git-agent/domain/project"
	infraHook "github.com/gitagenthq/git-agent/infrastructure/hook"
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

	result, err := exec.Execute(context.Background(), []string{"conventional"}, compositeInput(validCommitMessage()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestCompositeExecutor_InvalidMessage_Blocked(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), []string{"conventional"}, compositeInput("bad commit message"))
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

	result, err := exec.Execute(context.Background(), []string{"conventional"}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stderr, "error:") {
		t.Errorf("expected 'error:' prefix in stderr, got: %s", result.Stderr)
	}
}

func TestCompositeExecutor_Empty_PassesWithoutValidation(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), []string{"empty"}, compositeInput("bad commit message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0 for empty hook, got %d", result.ExitCode)
	}
}

func TestCompositeExecutor_NilSlice_PassesWithoutValidation(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), nil, compositeInput("bad commit message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0 for nil hooks, got %d", result.ExitCode)
	}
}

func TestCompositeExecutor_EmptySlice_PassesWithoutValidation(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), []string{}, compositeInput("bad commit message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0 for empty hooks slice, got %d", result.ExitCode)
	}
}

func TestCompositeExecutor_ValidMessage_ShellHookBlocks(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\necho 'custom check failed' >&2\nexit 1\n", 0o755)
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), []string{script}, compositeInput(validCommitMessage()))
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

	result, err := exec.Execute(context.Background(), []string{script}, compositeInput(validCommitMessage()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestCompositeExecutor_WarningsOnly_Passes(t *testing.T) {
	msg := "feat: added user authentication\n\n- add login endpoint\n\nThis introduces authentication support."
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), []string{"conventional"}, compositeInput(msg))
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

func TestCompositeExecutor_MultipleHooks_ConventionalThenShell(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\nexit 0\n", 0o755)
	exec := infraHook.NewCompositeHookExecutor()

	result, err := exec.Execute(context.Background(), []string{"conventional", script}, compositeInput(validCommitMessage()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0 for combined hooks, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestCompositeExecutor_MultipleHooks_FirstFails(t *testing.T) {
	script := writeTempScript(t, "#!/bin/sh\nexit 0\n", 0o755)
	exec := infraHook.NewCompositeHookExecutor()

	// Conventional validation fails on invalid message; shell hook should not run.
	result, err := exec.Execute(context.Background(), []string{"conventional", script}, compositeInput("bad message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code when first hook fails")
	}
}

func TestCompositeExecutor_ScopeWhitelist_Blocked(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()
	input := domainHook.HookInput{
		CommitMessage: validCommitMessage(), // uses no scope
		StagedFiles:   []string{"auth.go"},
		Config: domainProject.Config{
			Scopes: []domainProject.Scope{
				{Name: "app"},
				{Name: "cli"},
			},
		},
	}
	// Replace message with one that has a disallowed scope
	input.CommitMessage = "docs(code-graph-design): restructure\n\n- restructure docs\n\nReorganises docs.\n\nCo-Authored-By: Bot <bot@example.com>"

	result, err := exec.Execute(context.Background(), []string{"conventional"}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode == 0 {
		t.Error("expected disallowed scope to be blocked")
	}
	if !strings.Contains(result.Stderr, "not in the allowed list") {
		t.Errorf("expected scope error in stderr, got: %s", result.Stderr)
	}
}

func TestCompositeExecutor_ScopeWhitelist_Allowed(t *testing.T) {
	exec := infraHook.NewCompositeHookExecutor()
	input := domainHook.HookInput{
		CommitMessage: "feat(app): add login\n\n- add login endpoint\n\nThis adds login support.\n\nCo-Authored-By: Bot <bot@example.com>",
		StagedFiles:   []string{"auth.go"},
		Config: domainProject.Config{
			Scopes: []domainProject.Scope{
				{Name: "app"},
				{Name: "cli"},
			},
		},
	}

	result, err := exec.Execute(context.Background(), []string{"conventional"}, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected allowed scope to pass, got exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}
}
