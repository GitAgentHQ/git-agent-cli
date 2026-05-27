package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/cmd"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/project"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

func TestCommitCmd_DryRunFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"commit", "--dry-run"})
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--dry-run flag not recognized: %v", err)
	}
}

// TestCommit_RenderBudgetExhausted covers REQ-006: when the application
// surfaces a wrapped *commit.PlannerBudgetExhaustedError, the cmd layer
// renders an actionable diagnostic that names the model, the ceiling, and
// at least two concrete remediations, then exits with code 1. The renderer
// must NOT leak the old "max_tokens=N" doubling vocabulary, which would
// suggest a knob the user no longer controls.
func TestCommit_RenderBudgetExhausted(t *testing.T) {
	var stderr bytes.Buffer
	err := fmt.Errorf("plan commits: %w", &commit.PlannerBudgetExhaustedError{
		Model:   "deepseek-v4-flash",
		Ceiling: 16384,
	})

	rendered := cmd.RenderCommitError(&stderr, err)

	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "model=deepseek-v4-flash") {
		t.Errorf("stderr missing model identity, got: %q", stderrStr)
	}
	if !strings.Contains(stderrStr, "ceiling=16384") {
		t.Errorf("stderr missing ceiling, got: %q", stderrStr)
	}
	remediations := []string{
		"--max-diff-lines",
		"--max-diff-bytes",
		"--intent",
		"try a more capable model",
		"commit smaller batches",
	}
	hits := 0
	for _, r := range remediations {
		if strings.Contains(stderrStr, r) {
			hits++
		}
	}
	if hits < 2 {
		t.Errorf("expected at least 2 of %v in stderr, got %d hits in: %q", remediations, hits, stderrStr)
	}
	if strings.Contains(stderrStr, "max_tokens=32768") {
		t.Errorf("stderr leaks legacy doubling vocabulary: %q", stderrStr)
	}

	var exitErr *agentErrors.ExitCodeError
	if !errors.As(rendered, &exitErr) {
		t.Fatalf("expected *agentErrors.ExitCodeError, got %T: %v", rendered, rendered)
	}
	if exitErr.Code != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.Code)
	}
}

// TestCommit_WiresConfigToConstructors covers REQ-001, REQ-004, REQ-008
// wiring: the resolved request_timeout / heartbeat_interval values reach the
// openai client, and plan_fallback flips the CommitService's heuristic
// planner on or off. Without this test the three batches of infrastructure
// work could land without ever being reachable from the runtime path.
func TestCommit_WiresConfigToConstructors(t *testing.T) {
	userCfgDir := t.TempDir()
	userCfgPath := filepath.Join(userCfgDir, "git-agent", "config.yml")
	if err := os.MkdirAll(filepath.Dir(userCfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	userYAML := "" +
		"api_key: test-key\n" +
		"base_url: http://127.0.0.1:0\n" +
		"model: test-model\n" +
		"request_timeout: 5s\n" +
		"heartbeat_interval: 2s\n"
	if err := os.WriteFile(userCfgPath, []byte(userYAML), 0o644); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	resolved, err := infraConfig.Resolve(context.Background(), infraConfig.ProviderConfig{}, userCfgPath)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.RequestTimeout != 5*time.Second {
		t.Fatalf("resolved.RequestTimeout = %s, want 5s", resolved.RequestTimeout)
	}
	if resolved.HeartbeatInterval != 2*time.Second {
		t.Fatalf("resolved.HeartbeatInterval = %s, want 2s", resolved.HeartbeatInterval)
	}

	heuristicCfg := &project.Config{PlanFallback: project.PlanFallbackHeuristic}
	llmH, svcH := cmd.BuildCommitDepsForTest(resolved, heuristicCfg)
	if got := cmd.OpenAIRequestTimeoutForTest(llmH); got != 5*time.Second {
		t.Errorf("llm request timeout = %s, want 5s", got)
	}
	if got := cmd.OpenAIHeartbeatIntervalForTest(llmH); got != 2*time.Second {
		t.Errorf("llm heartbeat interval = %s, want 2s", got)
	}
	if cmd.CommitServiceHeuristicPlannerForTest(svcH) == nil {
		t.Errorf("plan_fallback=heuristic must produce a non-nil heuristicPlanner on the service")
	}

	noneCfg := &project.Config{PlanFallback: project.PlanFallbackNone}
	_, svcN := cmd.BuildCommitDepsForTest(resolved, noneCfg)
	if cmd.CommitServiceHeuristicPlannerForTest(svcN) != nil {
		t.Errorf("plan_fallback=none must leave heuristicPlanner nil on the service")
	}
}

func TestCommitCmd_AllFlagRemoved(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := cmd.ExecuteArgs([]string{"commit", "--all"})
	if err == nil {
		t.Fatal("expected error for removed --all flag, got nil")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected 'unknown flag' error for --all, got: %v", err)
	}
}
