package hook

import "context"

// HookExecutor runs a git-agent hook script with the given input.
type HookExecutor interface {
	Execute(ctx context.Context, hookPath string, input HookInput) (*HookResult, error)
}
