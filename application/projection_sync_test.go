package application_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infragraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// cleanGit is a git fake for a committed working tree: reconcile finds no
// unexplained changes (DiffNameOnly empty), so no out-of-band Events are
// appended, isolating sync's staleness behavior. HashObject serves stable blobs.
type cleanGit struct {
	graph.GraphGitClient
	blobs map[string]string
}

func (g *cleanGit) DiffNameOnly(context.Context) ([]string, error) { return nil, nil }

func (g *cleanGit) HashObject(_ context.Context, path string) (string, error) {
	if h, ok := g.blobs[path]; ok {
		return h, nil
	}
	return "blob-" + path, nil
}

// seededOutcome appends one Outcome (test-run) Event through the real append
// path. An Outcome touches no files, so it produces an action but no event_files
// row — the case the projected high-water mark must still account for.
func seededOutcome(t *testing.T, repo *infragraph.SQLiteRepository, source graph.EventSource, instanceID string, recordedAt int64) graph.EventRecord {
	t.Helper()
	code := 0
	rec := graph.EventRecord{
		EventID:        fmt.Sprintf("outcome-%s-%d", instanceID, recordedAt),
		RecordedAt:     recordedAt,
		Source:         source,
		InstanceID:     instanceID,
		Kind:           graph.EventKindOutcome,
		ToolName:       "Bash",
		Command:        "go test ./...",
		ExitCode:       &code,
		ExitCodeSource: "reported",
		IsTest:         true,
		TestName:       "go test ./...",
		PayloadRaw:     []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`),
	}
	got, err := repo.AppendEvent(context.Background(), rec)
	if err != nil {
		t.Fatalf("AppendEvent outcome: %v", err)
	}
	return got
}

func TestSyncIfStale_IdempotentWhenTailIsOutcome(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)
	gitFake := &cleanGit{blobs: map[string]string{"a.go": "blob-a"}}

	// seq 1: a file edit (produces an event_files row).
	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")
	// seq 2: an Outcome — touches no file, so leaves NO event_files row. Before the
	// fix this pegged the staleness mark at seq 1 forever.
	seededOutcome(t, repo, graph.EventSourceClaudeCode, "agent-A", 1001)

	if _, err := application.SyncEventLog(ctx, repo, gitFake); err != nil {
		t.Fatalf("SyncEventLog: %v", err)
	}
	if got := countRows(t, repo, "actions"); got != 2 {
		t.Fatalf("after rebuild: actions = %d, want 2", got)
	}

	// Every subsequent sync is a no-op: the explicit high-water mark sits at the
	// tail (seq 2) even though the tail Outcome produced no event_files row.
	for i := 1; i <= 3; i++ {
		summary, err := application.SyncIfStale(ctx, repo, gitFake)
		if err != nil {
			t.Fatalf("SyncIfStale #%d: %v", i, err)
		}
		if !summary.UpToDate {
			t.Errorf("SyncIfStale #%d: UpToDate = false, want true", i)
		}
		if summary.Replayed {
			t.Errorf("SyncIfStale #%d: Replayed = true, want false", i)
		}
		if got := countRows(t, repo, "actions"); got != 2 {
			t.Fatalf("after sync #%d: actions = %d, want 2 (no duplicate)", i, got)
		}
	}
}

func TestSyncIfStale_CatchesUpThenStaysIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)
	gitFake := &cleanGit{blobs: map[string]string{"a.go": "blob-a"}}

	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")
	if _, err := application.SyncEventLog(ctx, repo, gitFake); err != nil {
		t.Fatalf("SyncEventLog: %v", err)
	}
	if got := countRows(t, repo, "actions"); got != 1 {
		t.Fatalf("after rebuild: actions = %d, want 1", got)
	}

	// A new Outcome makes the projections genuinely stale.
	seededOutcome(t, repo, graph.EventSourceClaudeCode, "agent-A", 1001)
	summary, err := application.SyncIfStale(ctx, repo, gitFake)
	if err != nil {
		t.Fatalf("SyncIfStale: %v", err)
	}
	if summary.UpToDate || !summary.Replayed {
		t.Errorf("catch-up: UpToDate=%v Replayed=%v, want false/true", summary.UpToDate, summary.Replayed)
	}
	if got := countRows(t, repo, "actions"); got != 2 {
		t.Fatalf("after catch-up: actions = %d, want 2", got)
	}

	// Now idempotent again.
	summary, err = application.SyncIfStale(ctx, repo, gitFake)
	if err != nil {
		t.Fatalf("SyncIfStale (2nd): %v", err)
	}
	if !summary.UpToDate {
		t.Errorf("2nd sync: UpToDate = false, want true")
	}
	if got := countRows(t, repo, "actions"); got != 2 {
		t.Errorf("after 2nd sync: actions = %d, want 2", got)
	}
}
