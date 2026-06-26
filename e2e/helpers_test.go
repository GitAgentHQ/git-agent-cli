package e2e_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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

// gitAgentSeparated runs git-agent with stdout and stderr captured separately.
// Used where a command's contract distinguishes the two streams (e.g. diagnose
// writes its diagnosis report to stdout while errors go to stderr).
func gitAgentSeparated(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	c := exec.Command(agentBin, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	code := 0
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("unexpected error running git-agent: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// gitAgentStdin runs git-agent with the given bytes piped to stdin, simulating
// a PostToolUse hook payload. Returns combined output and exit code.
func gitAgentStdin(t *testing.T, dir string, stdin []byte, args ...string) (string, int) {
	t.Helper()
	c := exec.Command(agentBin, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
	c.Stdin = bytes.NewReader(stdin)
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
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newStallServer returns an httptest server that hijacks every connection and
// blocks until the client closes its end of the socket. The returned hit
// channel receives a value when the first request lands, letting callers gate
// follow-up actions (e.g. SIGINT) on the subprocess actually reaching the
// LLM call instead of relying on a wall-clock sleep.
func newStallServer(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("http.ResponseWriter does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}))
	return srv, hit
}

// newFastLLMServer returns an httptest server that responds to every
// chat-completion request with a canned message-generation payload. The
// configurable delay defaults to 0 — used by the small-diff happy path to
// ensure no heartbeat ticks fire. The server's content is fixed because the
// regression test cares only about turn-around time and exit code, not the
// commit text itself.
func newFastLLMServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	const body = `{
  "id": "chatcmpl-fast",
  "object": "chat.completion",
  "created": 0,
  "model": "test-model",
  "choices": [
    {"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": "{\"title\":\"feat(cli): add a line\",\"bullets\":[\"Add a new line to readme\"],\"explanation\":\"Adds one line of content for the regression fixture.\"}"}}
  ]
}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
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
