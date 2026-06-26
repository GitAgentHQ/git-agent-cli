package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/domain/graph"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

// setupVerifyRepo creates a git repo in a temp dir, appends n valid Events to its
// graph.db, and chdirs into it. It returns the repo root.
func setupVerifyRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)

	ctx := context.Background()
	dbPath := filepath.Join(dir, ".git-agent", "graph.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	client := infraGraph.NewSQLiteClient(dbPath)
	repo := infraGraph.NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	for i := 1; i <= n; i++ {
		if _, err := repo.AppendEvent(ctx, graph.EventRecord{
			EventID:    "evt-" + string(rune('0'+i)),
			RecordedAt: int64(1000 + i),
			Source:     graph.EventSourceClaudeCode,
			InstanceID: "agent-1",
			Kind:       graph.EventKindTool,
			ToolName:   "Edit",
			PayloadRaw: []byte(`{"file_path":"a.go"}`),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return dir
}

func newVerifyCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().Bool("json", false, "json output")
	return cmd
}

func TestGraphVerify_CleanChainExitsZero(t *testing.T) {
	setupVerifyRepo(t, 3)
	if err := runGraphVerify(newVerifyCmd(t), nil); err != nil {
		t.Fatalf("expected nil error (exit 0) on clean chain, got: %v", err)
	}
}

func TestGraphVerify_BrokenChainExitsFour(t *testing.T) {
	dir := setupVerifyRepo(t, 3)

	ctx := context.Background()
	client := infraGraph.NewSQLiteClient(filepath.Join(dir, ".git-agent", "graph.db"))
	repo := infraGraph.NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := repo.Client().DB().ExecContext(ctx,
		`UPDATE events SET payload_raw = ? WHERE seq = 2`, `{"tampered":true}`,
	); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	repo.Close()

	err := runGraphVerify(newVerifyCmd(t), nil)
	if err == nil {
		t.Fatal("expected an error on a broken chain")
	}
	var exitErr *agentErrors.ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *agentErrors.ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != 4 {
		t.Errorf("exit code = %d, want 4", exitErr.Code)
	}
}
