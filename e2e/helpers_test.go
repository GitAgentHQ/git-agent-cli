package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var agentBin string

func TestMain(m *testing.M) {
	bin, err := buildBinary()
	if err != nil {
		panic("failed to build git-agent binary: " + err.Error())
	}
	agentBin = bin
	defer os.Remove(bin)
	os.Exit(m.Run())
}

func buildBinary() (string, error) {
	tmp, err := os.MkdirTemp("", "git-agent-e2e-*")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(tmp, "git-agent")
	c := exec.Command("go", "build", "-o", bin, "github.com/gitagenthq/git-agent")
	if out, err := c.CombinedOutput(); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	return bin, nil
}

// gitAgent runs the git-agent binary with the given args in the given directory, with no user config.
// Returns combined output and exit code.
func gitAgent(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	return gitAgentEnv(t, dir, nil, args...)
}

// gitAgentEnv runs the git-agent binary with additional environment variables.
func gitAgentEnv(t *testing.T, dir string, env []string, args ...string) (string, int) {
	t.Helper()
	c := exec.Command(agentBin, args...)
	c.Dir = dir
	// Isolate from user config by default.
	c.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
	c.Env = append(c.Env, env...)
	out, err := c.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("unexpected error running git-agent: %v", err)
		}
	}
	return string(out), code
}

// newGitRepo creates a temp directory initialised as a git repository.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	return dir
}

// writeFile writes content to path (creating parent dirs as needed).
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
