package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
		{"llm-model", ""},
		{"llm-base-url", ""},
		{"llm-api-key", ""},
		{"force", false},
		{"top", 5},
		{"json", false},
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
	// --llm-timeout is a duration.
	c.Flags().Duration("llm-timeout", 0, "")
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

func TestBuildDiagnoseReranker_NoKeyErrorsExitOne(t *testing.T) {
	withIsolatedConfig(t, func() {
		c := newDiagnoseCmdForTest(t)
		// No --llm-api-key, no user/git config, no build-time creds, no --free:
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

func TestBuildDiagnoseReranker_FlagOverrideModel(t *testing.T) {
	withIsolatedConfig(t, func() {
		c := newDiagnoseCmdForTest(t)
		_ = c.Flags().Set("llm-api-key", "test-key")
		_ = c.Flags().Set("llm-model", "claude-opus-4-8")
		_ = c.Flags().Set("llm-base-url", "https://example.test/v1")

		reranker, err := buildDiagnoseReranker(c)
		if err != nil {
			t.Fatalf("buildDiagnoseReranker: %v", err)
		}
		if reranker == nil {
			t.Fatal("expected a non-nil reranker when --llm-api-key is set")
		}
	})
}

func TestDiagnoseFlagsRegistered(t *testing.T) {
	// Sanity: the public diagnoseCmd exposes the --llm-* surface.
	for _, flag := range []string{"llm", "llm-model", "llm-base-url", "llm-api-key", "llm-timeout", "top"} {
		if diagnoseCmd.Flags().Lookup(flag) == nil {
			t.Errorf("diagnoseCmd missing flag %q", flag)
		}
	}
}
