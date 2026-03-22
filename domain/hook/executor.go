package hook

import "context"

// HookExecutor runs git-agent hooks with the given input.
// hooks is an ordered list of hook names/paths. Each entry is "conventional" for
// built-in validation or a file path for a custom shell script. Empty slice = no validation.
type HookExecutor interface {
	Execute(ctx context.Context, hooks []string, input HookInput) (*HookResult, error)
}
