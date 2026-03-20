package hook

import "context"

// HookExecutor runs a git-agent hook with the given input.
// hookType is "conventional", "empty", "" for built-in types, or a file path for custom scripts.
type HookExecutor interface {
	Execute(ctx context.Context, hookType string, input HookInput) (*HookResult, error)
}
