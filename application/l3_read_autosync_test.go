package application_test

import (
	"context"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
)

// TestL3Read_AutoSyncShowsJustCapturedEvent asserts the CQRS read-side contract:
// after capture appends an event (producer-only, no projection fold), the L3
// read path's SyncIfStale folds it so timeline reflects it without a manual
// sync step.
func TestL3Read_AutoSyncShowsJustCapturedEvent(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)
	gitFake := &cleanGit{blobs: map[string]string{"a.go": "blob-a"}}

	// Producer: capture appends one Outcome event. No projection replay yet.
	seededOutcome(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000)

	// Precondition: projections lag the Event Log (1 event, 0 folded actions).
	if got := countRows(t, repo, "actions"); got != 0 {
		t.Fatalf("precondition: actions = %d, want 0 (not yet folded)", got)
	}

	// Read-side auto-sync: the L3 read path calls SyncIfStale before reading.
	if _, err := application.SyncIfStale(ctx, repo, gitFake); err != nil {
		t.Fatalf("SyncIfStale on read path: %v", err)
	}

	// The timeline now reflects the captured event — no manual sync needed.
	tr, err := repo.Timeline(ctx, graph.TimelineRequest{Top: 50})
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if tr.TotalSessions != 1 || tr.TotalActions != 1 {
		t.Errorf("timeline = %d sessions/%d actions, want 1/1", tr.TotalSessions, tr.TotalActions)
	}
}

// TestL3Read_AutoSyncNoOpOnEmptyEventLog asserts an empty Event Log does not
// error the read path: SyncIfStale short-circuits at 0>=0.
func TestL3Read_AutoSyncNoOpOnEmptyEventLog(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)
	gitFake := &cleanGit{}

	summary, err := application.SyncIfStale(ctx, repo, gitFake)
	if err != nil {
		t.Fatalf("SyncIfStale on empty log: %v", err)
	}
	if !summary.UpToDate {
		t.Errorf("empty log: UpToDate = false, want true (cheap no-op)")
	}

	tr, err := repo.Timeline(ctx, graph.TimelineRequest{Top: 50})
	if err != nil {
		t.Fatalf("Timeline on empty log: %v", err)
	}
	if tr.TotalSessions != 0 || tr.TotalActions != 0 {
		t.Errorf("empty log timeline = %d/%d, want 0/0", tr.TotalSessions, tr.TotalActions)
	}
}

// TestL3Read_AutoSyncIdempotentOnCurrent asserts that when projections are
// already current, the read path's SyncIfStale is a no-op (no duplicate fold).
func TestL3Read_AutoSyncIdempotentOnCurrent(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)
	gitFake := &cleanGit{blobs: map[string]string{"a.go": "blob-a"}}

	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")
	if _, err := application.SyncEventLog(ctx, repo, gitFake); err != nil {
		t.Fatalf("SyncEventLog: %v", err)
	}
	before := countRows(t, repo, "actions")

	// Read path auto-syncs — already current, so a no-op.
	summary, err := application.SyncIfStale(ctx, repo, gitFake)
	if err != nil {
		t.Fatalf("SyncIfStale: %v", err)
	}
	if !summary.UpToDate || summary.Replayed {
		t.Errorf("current: UpToDate=%v Replayed=%v, want true/false", summary.UpToDate, summary.Replayed)
	}
	if got := countRows(t, repo, "actions"); got != before {
		t.Errorf("current: actions = %d, want %d (no duplicate)", got, before)
	}
}
