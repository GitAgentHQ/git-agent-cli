package hook

import "context"

// HookExecutor runs a ga hook script with the given input.
type HookExecutor interface {
	Execute(ctx context.Context, hookPath string, input HookInput) (*HookResult, error)
}
