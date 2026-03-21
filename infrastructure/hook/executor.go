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

func NewShellHookExecutor() domainHook.HookExecutor {
	return &shellHookExecutor{}
}

func (e *shellHookExecutor) Execute(ctx context.Context, hookType string, input domainHook.HookInput) (*domainHook.HookResult, error) {
	hookPath := hookType
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
