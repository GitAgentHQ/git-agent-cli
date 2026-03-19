package hook

// HookResult holds the outcome of a hook execution.
type HookResult struct {
	ExitCode int
	Stderr   string
}
