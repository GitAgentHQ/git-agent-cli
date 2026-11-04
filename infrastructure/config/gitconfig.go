package config

import (
	"context"
	"os/exec"
	"strings"
)

// ReadGitConfig reads a git-agent.* key from the local git config.
// Returns ("", nil) when the key is not set.
func ReadGitConfig(ctx context.Context, key string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "config", "--local", "--get", "git-agent."+key).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil // key not set
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}
