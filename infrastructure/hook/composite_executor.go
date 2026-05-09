package hook

import (
	"context"
	"strings"

	domainCommit "github.com/gitagenthq/git-agent/domain/commit"
	domainHook "github.com/gitagenthq/git-agent/domain/hook"
	domainProject "github.com/gitagenthq/git-agent/domain/project"
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
	// Runs even with no hooks configured — that is the point of the policy flag.
	if input.Config.RequireModelCoAuthor {
		if result := e.runModelCoAuthorCheck(input); result.ExitCode != 0 {
			return result, nil
		}
	}

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

func (e *compositeHookExecutor) runModelCoAuthorCheck(input domainHook.HookInput) *domainHook.HookResult {
	domains := append([]string(nil), domainProject.DefaultModelCoAuthorDomains...)
	domains = append(domains, input.Config.ModelCoAuthorDomains...)

	validation := domainCommit.ValidateModelCoAuthor(input.CommitMessage, domains)
	if !validation.HasErrors() {
		return &domainHook.HookResult{ExitCode: 0}
	}
	return &domainHook.HookResult{ExitCode: 1, Stderr: formatIssueLines("error: ", validation.Errors())}
}

func (e *compositeHookExecutor) runValidation(input domainHook.HookInput) *domainHook.HookResult {
	validation := domainCommit.ValidateConventional(input.CommitMessage, input.Config.ScopeNames())

	if validation.HasErrors() {
		stderr := formatIssueLines("error: ", validation.Errors()) +
			formatIssueLines("warning: ", validation.Warnings())
		return &domainHook.HookResult{ExitCode: 1, Stderr: stderr}
	}

	return &domainHook.HookResult{ExitCode: 0, Stderr: formatIssueLines("warning: ", validation.Warnings())}
}

// formatIssueLines renders one "<prefix><msg>\n" line per message, or "" for none.
func formatIssueLines(prefix string, msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, msg := range msgs {
		sb.WriteString(prefix)
		sb.WriteString(msg)
		sb.WriteString("\n")
	}
	return sb.String()
}
