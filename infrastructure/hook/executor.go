package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	domainHook "github.com/gitagenthq/git-agent/domain/hook"
)

type shellHookExecutor struct{}

// NewShellHookExecutor returns a HookExecutor that runs each entry in hooks as a
// shell script path in order, failing fast on the first non-zero exit.
func NewShellHookExecutor() domainHook.HookExecutor {
	return &shellHookExecutor{}
}

// Execute implements domainHook.HookExecutor for the shell executor.
// Each entry in hooks is treated as a file path to execute.
func (e *shellHookExecutor) Execute(ctx context.Context, hooks []string, input domainHook.HookInput) (*domainHook.HookResult, error) {
	for _, path := range hooks {
		result, err := e.execute(ctx, path, input)
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 {
			return result, nil
		}
	}
	return &domainHook.HookResult{ExitCode: 0}, nil
}

// execute runs a single hook script at hookPath.
func (e *shellHookExecutor) execute(ctx context.Context, hookPath string, input domainHook.HookInput) (*domainHook.HookResult, error) {
	info, err := os.Stat(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &domainHook.HookResult{ExitCode: 0}, nil
		}
		return nil, err
	}

	if info.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("hook is not executable: %s", hookPath)
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, hookPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, runErr
		}
	}

	return &domainHook.HookResult{
		ExitCode: exitCode,
		Stderr:   stderr.String(),
	}, nil
}
