package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var gaBin string

func TestMain(m *testing.M) {
	bin, err := buildBinary()
	if err != nil {
		panic("failed to build ga binary: " + err.Error())
	}
	gaBin = bin
	defer os.Remove(bin)
	os.Exit(m.Run())
}

func buildBinary() (string, error) {
	tmp, err := os.MkdirTemp("", "ga-e2e-*")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(tmp, "ga")
	c := exec.Command("go", "build", "-o", bin, "github.com/fradser/ga-cli")
	if out, err := c.CombinedOutput(); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	return bin, nil
}

// ga runs the ga binary with the given args and returns combined output and exit code.
func ga(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	c := exec.Command(gaBin, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("unexpected error running ga: %v", err)
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
