package hook

import (
	"context"
	"strings"

	domainCommit "github.com/fradser/git-agent/domain/commit"
	domainHook "github.com/fradser/git-agent/domain/hook"
)

type compositeHookExecutor struct {
	shell domainHook.HookExecutor
}

// NewCompositeHookExecutor returns a HookExecutor that first runs the
// built-in Go-native conventional commit validator, then delegates to
// the shell hook executor if validation passes.
func NewCompositeHookExecutor() domainHook.HookExecutor {
	return &compositeHookExecutor{shell: NewShellHookExecutor()}
}

func (e *compositeHookExecutor) Execute(ctx context.Context, hookPath string, input domainHook.HookInput) (*domainHook.HookResult, error) {
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
		return &domainHook.HookResult{ExitCode: 1, Stderr: sb.String()}, nil
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

	shellResult, err := e.shell.Execute(ctx, hookPath, input)
	if err != nil {
		return nil, err
	}

	if warnText != "" {
		shellResult.Stderr = warnText + shellResult.Stderr
	}
	return shellResult, nil
}
