package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

// newDiagnoseCmdForTest builds a cobra.Command with the diagnose flags registered,
// for unit-testing flag parsing and the reranker wiring without invoking the
// command's RunE.
func newDiagnoseCmdForTest(t *testing.T) *cobra.Command {
	t.Helper()
	c := &cobra.Command{}
	c.SetContext(context.Background())
	for _, f := range []struct {
		name string
		def  any
	}{
		{"file", []string{}},
		{"llm", false},
		{"force", false},
		{"top", 5},
		{"json", false},
		{"text", false},
	} {
		switch d := f.def.(type) {
		case bool:
			c.Flags().Bool(f.name, d, "")
		case string:
			c.Flags().String(f.name, d, "")
		case int:
			c.Flags().Int(f.name, d, "")
		case []string:
			c.Flags().StringSlice(f.name, d, "")
		}
	}
	return c
}

// withIsolatedConfig runs fn with HOME pointed at an empty temp dir and from
// inside a fresh git repo, so no real user/git config leaks into the resolver.
func withIsolatedConfig(t *testing.T, fn func()) {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	prevHome := os.Getenv("HOME")
	prevDir, _ := os.Getwd()
	if err := os.Setenv("HOME", dir); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	defer os.Setenv("HOME", prevHome)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(prevDir)
	fn()
}

// writeUserConfig writes a YAML user config into the isolated HOME so the
// resolver picks up diagnose-* keys without any CLI flags.
func writeUserConfig(t *testing.T, body string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	path := filepath.Join(home, ".config", "git-agent", "config.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestBuildDiagnoseReranker_NoKeyErrorsExitOne(t *testing.T) {
	withIsolatedConfig(t, func() {
		c := newDiagnoseCmdForTest(t)
		// No diagnose-api-key, no main api-key, no build-time creds, no --free:
		// the reranker must refuse with an actionable exit-code-1 error.
		_, err := buildDiagnoseReranker(c)
		if err == nil {
			t.Fatal("expected an error when no API key is configured")
		}
		var exitErr *agentErrors.ExitCodeError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected *agentErrors.ExitCodeError, got %T: %v", err, err)
		}
		if exitErr.Code != 1 {
			t.Errorf("exit code = %d, want 1", exitErr.Code)
		}
	})
}

func TestBuildDiagnoseReranker_ConfigKeyBuildsReranker(t *testing.T) {
	withIsolatedConfig(t, func() {
		writeUserConfig(t, "api_key: test-key\n"+
			"diagnose_model: claude-opus-4-8\n"+
			"diagnose_base_url: https://example.test/v1\n")
		c := newDiagnoseCmdForTest(t)

		reranker, err := buildDiagnoseReranker(c)
		if err != nil {
			t.Fatalf("buildDiagnoseReranker: %v", err)
		}
		if reranker == nil {
			t.Fatal("expected a non-nil reranker when diagnose-api-key (via main api_key fallback) is set")
		}
	})
}

func TestDiagnoseFlagsRegistered(t *testing.T) {
	// Sanity: the public diagnoseCmd exposes only behavioral flags — provider
	// values come from config keys, not flags.
	for _, flag := range []string{"file", "llm", "force", "top", "json", "text"} {
		if diagnoseCmd.Flags().Lookup(flag) == nil {
			t.Errorf("diagnoseCmd missing flag %q", flag)
		}
	}
	for _, flag := range []string{"llm-model", "llm-base-url", "llm-api-key", "llm-timeout"} {
		if diagnoseCmd.Flags().Lookup(flag) != nil {
			t.Errorf("diagnoseCmd must not expose removed flag %q (use a config key)", flag)
		}
	}
}
