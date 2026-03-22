package hook

import (
	"context"
	"strings"

	domainCommit "github.com/gitagenthq/git-agent/domain/commit"
	domainHook "github.com/gitagenthq/git-agent/domain/hook"
)

type compositeHookExecutor struct {
	shell *shellHookExecutor
}

// NewCompositeHookExecutor returns a HookExecutor that iterates over hooks in order,
// failing fast on the first block. Each hook entry is:
//   - "conventional": run Go-native ValidateConventional
//   - any other value: treat as file path and run as shell script
//
// An empty slice passes immediately (no validation).
func NewCompositeHookExecutor() domainHook.HookExecutor {
	return &compositeHookExecutor{shell: &shellHookExecutor{}}
}

func (e *compositeHookExecutor) Execute(ctx context.Context, hooks []string, input domainHook.HookInput) (*domainHook.HookResult, error) {
	if len(hooks) == 0 {
		return &domainHook.HookResult{ExitCode: 0}, nil
	}

	var combinedWarnings strings.Builder
	for _, h := range hooks {
		switch h {
		case "", "empty":
			// no-op entry; skip
			continue
		case "conventional":
			result := e.runValidation(input)
			if result.ExitCode != 0 {
				return result, nil
			}
			if result.Stderr != "" {
				combinedWarnings.WriteString(result.Stderr)
			}
		default:
			shellResult, err := e.shell.execute(ctx, h, input)
			if err != nil {
				return nil, err
			}
			if shellResult.ExitCode != 0 {
				return shellResult, nil
			}
			if shellResult.Stderr != "" {
				combinedWarnings.WriteString(shellResult.Stderr)
			}
		}
	}
	return &domainHook.HookResult{ExitCode: 0, Stderr: combinedWarnings.String()}, nil
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
