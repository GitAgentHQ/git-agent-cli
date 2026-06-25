package graph

import (
	"context"
	"path/filepath"
	"testing"

	domaingraph "github.com/gitagenthq/git-agent/domain/graph"
)

func TestTimelineSinceFiltersActionsNotSessionStart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "timeline.db")
	client := NewSQLiteClient(dbPath)
	repo := NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	if err := repo.UpsertSession(ctx, domaingraph.SessionNode{
		ID:        "session-1",
		Source:    "claude-code",
		StartedAt: 100,
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	if _, err := repo.CreateActionBatch(ctx, domaingraph.ActionNode{
		ID:           "session-1:1",
		SessionID:    "session-1",
		Sequence:     1,
		Tool:         "Edit",
		FilesChanged: []string{"old.go"},
		Timestamp:    150,
	}, []domaingraph.FileChange{{Path: "old.go", Additions: 1}}); err != nil {
		t.Fatalf("create old action: %v", err)
	}
	if _, err := repo.CreateActionBatch(ctx, domaingraph.ActionNode{
		ID:           "session-1:2",
		SessionID:    "session-1",
		Sequence:     2,
		Tool:         "Edit",
		FilesChanged: []string{"new.go"},
		Timestamp:    250,
	}, []domaingraph.FileChange{{Path: "new.go", Additions: 1}}); err != nil {
		t.Fatalf("create new action: %v", err)
	}

	result, err := repo.Timeline(ctx, domaingraph.TimelineRequest{Since: 200, Top: 10})
	if err != nil {
		t.Fatalf("Timeline() error = %v", err)
	}

	actionCount := 0
	if len(result.Sessions) > 0 {
		actionCount = len(result.Sessions[0].Actions)
	}

	if result.TotalSessions != 1 {
		t.Fatalf("Timeline should include session with post-cutoff action, got %d sessions", result.TotalSessions)
	}
	if actionCount != 1 || result.Sessions[0].Actions[0].ID != "session-1:2" {
		t.Fatalf("Timeline should return only post-cutoff action, got %+v", result.Sessions[0].Actions)
	}

	fileResult, err := repo.Timeline(ctx, domaingraph.TimelineRequest{Since: 200, File: "new.go", Top: 10})
	if err != nil {
		t.Fatalf("Timeline(file) error = %v", err)
	}
	if fileResult.TotalSessions != 1 || len(fileResult.Sessions[0].Actions) != 1 || fileResult.Sessions[0].Actions[0].ID != "session-1:2" {
		t.Fatalf("Timeline file filter should include only matching post-cutoff action, got %+v", fileResult.Sessions)
	}
}

func TestTimelineOrdersSessionsByLatestMatchingAction(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "timeline.db")
	client := NewSQLiteClient(dbPath)
	repo := NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	for _, session := range []domaingraph.SessionNode{
		{ID: "session-old-start-new-action", Source: "claude-code", StartedAt: 100},
		{ID: "session-new-start-old-action", Source: "claude-code", StartedAt: 300},
	} {
		if err := repo.UpsertSession(ctx, session); err != nil {
			t.Fatalf("upsert session: %v", err)
		}
	}
	if _, err := repo.CreateActionBatch(ctx, domaingraph.ActionNode{
		ID:           "session-old-start-new-action:1",
		SessionID:    "session-old-start-new-action",
		Sequence:     1,
		Tool:         "Edit",
		FilesChanged: []string{"newest.go"},
		Timestamp:    300,
	}, []domaingraph.FileChange{{Path: "newest.go", Additions: 1}}); err != nil {
		t.Fatalf("create newest action: %v", err)
	}
	if _, err := repo.CreateActionBatch(ctx, domaingraph.ActionNode{
		ID:           "session-new-start-old-action:1",
		SessionID:    "session-new-start-old-action",
		Sequence:     1,
		Tool:         "Edit",
		FilesChanged: []string{"older.go"},
		Timestamp:    250,
	}, []domaingraph.FileChange{{Path: "older.go", Additions: 1}}); err != nil {
		t.Fatalf("create older action: %v", err)
	}

	result, err := repo.Timeline(ctx, domaingraph.TimelineRequest{Since: 200, Top: 1})
	if err != nil {
		t.Fatalf("Timeline() error = %v", err)
	}
	if result.TotalSessions != 1 || result.Sessions[0].ID != "session-old-start-new-action" {
		t.Fatalf("Timeline should order by latest matching action, got %+v", result.Sessions)
	}
}
