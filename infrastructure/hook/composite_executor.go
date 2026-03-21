package hook

import (
	"context"
	"strings"

	domainCommit "github.com/gitagenthq/git-agent/domain/commit"
	domainHook "github.com/gitagenthq/git-agent/domain/hook"
)

type compositeHookExecutor struct {
	shell domainHook.HookExecutor
}

// NewCompositeHookExecutor returns a HookExecutor that dispatches based on hookType:
//   - "" or "empty": pass immediately (exit 0, no validation)
//   - "conventional": run Go-native ValidateConventional only
//   - any other value: treat as file path, run Go validation then shell executor
func NewCompositeHookExecutor() domainHook.HookExecutor {
	return &compositeHookExecutor{shell: NewShellHookExecutor()}
}

func (e *compositeHookExecutor) Execute(ctx context.Context, hookType string, input domainHook.HookInput) (*domainHook.HookResult, error) {
	switch hookType {
	case "", "empty":
		return &domainHook.HookResult{ExitCode: 0}, nil

	case "conventional":
		return e.runValidation(input), nil

	default:
		// Treat as file path: run Go validation first, then shell.
		validationResult := e.runValidation(input)
		if validationResult.ExitCode != 0 {
			return validationResult, nil
		}
		shellResult, err := e.shell.Execute(ctx, hookType, input)
		if err != nil {
			return nil, err
		}
		if validationResult.Stderr != "" {
			shellResult.Stderr = validationResult.Stderr + shellResult.Stderr
		}
		return shellResult, nil
	}
}

func (e *compositeHookExecutor) runValidation(input domainHook.HookInput) *domainHook.HookResult {
	validation := domainCommit.ValidateConventional(input.CommitMessage)

	if validation.HasErrors() {
		var sb strings.Builder
		for _, msg := range validation.Errors() {
			sb.WriteString("error: ")
			sb.WriteString(msg)
			sb.WriteString("\n")
		}
		for _, msg := range validation.Warnings() {
			sb.WriteString("warning: ")
			sb.WriteString(msg)
			sb.WriteString("\n")
		}
		return &domainHook.HookResult{ExitCode: 1, Stderr: sb.String()}
	}

	var warnText string
	if warnings := validation.Warnings(); len(warnings) > 0 {
		var sb strings.Builder
		for _, msg := range warnings {
			sb.WriteString("warning: ")
			sb.WriteString(msg)
			sb.WriteString("\n")
		}
		warnText = sb.String()
	}

	return &domainHook.HookResult{ExitCode: 0, Stderr: warnText}
}
