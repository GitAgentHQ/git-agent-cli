package config

import (
	"context"
	"os/exec"
	"strconv"
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

// ReadGitConfigBool reads a boolean git-agent.* key from the local git config.
// Returns (false, nil) when the key is not set.
func ReadGitConfigBool(ctx context.Context, key string) (bool, error) {
	val, err := ReadGitConfig(ctx, key)
	if err != nil || val == "" {
		return false, err
	}
	return strconv.ParseBool(val)
}
