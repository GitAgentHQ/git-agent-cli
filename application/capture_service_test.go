package application_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infragraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// setupCaptureTest creates a real SQLite repo in a temp dir and returns it
// along with a cleanup function. The schema is initialized.
func setupCaptureTest(t *testing.T) *infragraph.SQLiteRepository {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := infragraph.NewSQLiteClient(dbPath)
	repo := infragraph.NewSQLiteRepository(client)
	ctx := context.Background()
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestCaptureService_CaptureCreatesSessionAndAction(t *testing.T) {
	repo := setupCaptureTest(t)
	git := &mockGraphGitClient{
		diffNameOnlyResult: []string{"main.go"},
		hashObjectResults:  map[string]string{"main.go": "abc123"},
		diffForFilesResult: "+func main() {}",
	}
	svc := application.NewCaptureService(repo, git)

	result, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source: "claude-code",
		Tool:   "Edit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected capture to succeed, got skipped")
	}
	if result.SessionID == "" {
		t.Fatal("expected session ID to be set")
	}
	if result.ActionID == "" {
		t.Fatal("expected action ID to be set")
	}
	if len(result.FilesChanged) != 1 || result.FilesChanged[0] != "main.go" {
		t.Errorf("expected files_changed=[main.go], got %v", result.FilesChanged)
	}
	// Action ID should be "{sessionID}:1"
	if !strings.HasSuffix(result.ActionID, ":1") {
		t.Errorf("expected action ID to end with :1, got %q", result.ActionID)
	}
}

func TestCaptureService_DeltaCapture_OnlyNewChanges(t *testing.T) {
	repo := setupCaptureTest(t)

	// First capture: file A is modified.
	git := &mockGraphGitClient{
		diffNameOnlyResult: []string{"a.go"},
		hashObjectResults:  map[string]string{"a.go": "hash-a-v1"},
		diffForFilesResult: "+func a() {}",
	}
	svc := application.NewCaptureService(repo, git)

	r1, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source: "claude-code",
		Tool:   "Edit",
	})
	if err != nil {
		t.Fatalf("capture 1: %v", err)
	}
	if r1.Skipped {
		t.Fatal("capture 1: expected success, got skipped")
	}
	if len(r1.FilesChanged) != 1 || r1.FilesChanged[0] != "a.go" {
		t.Errorf("capture 1: expected [a.go], got %v", r1.FilesChanged)
	}

	// Second capture: A is unchanged (same hash), B is new.
	git.diffNameOnlyResult = []string{"a.go", "b.go"}
	git.hashObjectResults = map[string]string{
		"a.go": "hash-a-v1", // same as baseline
		"b.go": "hash-b-v1", // new file
	}
	git.diffForFilesResult = "+func b() {}"

	r2, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source: "claude-code",
		Tool:   "Write",
	})
	if err != nil {
		t.Fatalf("capture 2: %v", err)
	}
	if r2.Skipped {
		t.Fatal("capture 2: expected success, got skipped")
	}
	// Only b.go should be in the delta.
	if len(r2.FilesChanged) != 1 || r2.FilesChanged[0] != "b.go" {
		t.Errorf("capture 2: expected [b.go], got %v", r2.FilesChanged)
	}
}

func TestCaptureService_AppendsToExistingSession(t *testing.T) {
	repo := setupCaptureTest(t)
	git := &mockGraphGitClient{
		diffNameOnlyResult: []string{"main.go"},
		hashObjectResults:  map[string]string{"main.go": "hash-v1"},
		diffForFilesResult: "+v1",
	}
	svc := application.NewCaptureService(repo, git)

	r1, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source:     "claude-code",
		InstanceID: "pid-1",
		Tool:       "Edit",
	})
	if err != nil {
		t.Fatalf("capture 1: %v", err)
	}

	// Second capture with updated hash (so delta is detected).
	git.hashObjectResults = map[string]string{"main.go": "hash-v2"}
	git.diffForFilesResult = "+v2"

	r2, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source:     "claude-code",
		InstanceID: "pid-1",
		Tool:       "Edit",
	})
	if err != nil {
		t.Fatalf("capture 2: %v", err)
	}

	if r1.SessionID != r2.SessionID {
		t.Errorf("expected same session, got %q and %q", r1.SessionID, r2.SessionID)
	}
	if !strings.HasSuffix(r2.ActionID, ":2") {
		t.Errorf("expected second action to have sequence 2, got %q", r2.ActionID)
	}
}

func TestCaptureService_NewSessionAfterTimeout(t *testing.T) {
	repo := setupCaptureTest(t)
	ctx := context.Background()

	// Manually insert an old session that started 31 minutes ago.
	oldSession := graph.SessionNode{
		ID:         "old-session-id",
		Source:     "claude-code",
		InstanceID: "pid-1",
		StartedAt:  time.Now().Unix() - 31*60,
	}
	if err := repo.UpsertSession(ctx, oldSession); err != nil {
		t.Fatalf("upsert old session: %v", err)
	}

	git := &mockGraphGitClient{
		diffNameOnlyResult: []string{"main.go"},
		hashObjectResults:  map[string]string{"main.go": "hash-new"},
		diffForFilesResult: "+new",
	}
	svc := application.NewCaptureService(repo, git)

	result, err := svc.Capture(ctx, graph.CaptureRequest{
		Source:     "claude-code",
		InstanceID: "pid-1",
		Tool:       "Edit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionID == "old-session-id" {
		t.Error("expected a new session, got the old one")
	}
	if result.SessionID == "" {
		t.Fatal("expected a new session ID")
	}
}

func TestCaptureService_NoDiff_IsNoOp(t *testing.T) {
	repo := setupCaptureTest(t)
	git := &mockGraphGitClient{
		diffNameOnlyResult: []string{},
	}
	svc := application.NewCaptureService(repo, git)

	result, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source: "claude-code",
		Tool:   "Edit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Skipped {
		t.Error("expected skipped=true when no files changed")
	}
	if result.Reason != "no changes detected" {
		t.Errorf("expected reason 'no changes detected', got %q", result.Reason)
	}
}

func TestCaptureService_EndSession(t *testing.T) {
	repo := setupCaptureTest(t)
	ctx := context.Background()

	// Create a session first.
	git := &mockGraphGitClient{
		diffNameOnlyResult: []string{"main.go"},
		hashObjectResults:  map[string]string{"main.go": "hash1"},
		diffForFilesResult: "+code",
	}
	svc := application.NewCaptureService(repo, git)

	r, err := svc.Capture(ctx, graph.CaptureRequest{
		Source:     "claude-code",
		InstanceID: "pid-1",
		Tool:       "Edit",
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	sessionID := r.SessionID

	// End the session.
	if err := svc.EndSession(ctx, "claude-code", "pid-1"); err != nil {
		t.Fatalf("end session: %v", err)
	}

	// Verify session is ended: next capture should create a new session.
	git.hashObjectResults = map[string]string{"main.go": "hash2"}
	r2, err := svc.Capture(ctx, graph.CaptureRequest{
		Source:     "claude-code",
		InstanceID: "pid-1",
		Tool:       "Edit",
	})
	if err != nil {
		t.Fatalf("capture after end: %v", err)
	}
	if r2.SessionID == sessionID {
		t.Error("expected a new session after ending the previous one")
	}
}

func TestCaptureService_DiffTruncation(t *testing.T) {
	repo := setupCaptureTest(t)

	// Generate a diff larger than 100KB.
	largeDiff := strings.Repeat("x", 120*1024)

	git := &mockGraphGitClient{
		diffNameOnlyResult: []string{"big.go"},
		hashObjectResults:  map[string]string{"big.go": "hash-big"},
		diffForFilesResult: largeDiff,
	}
	svc := application.NewCaptureService(repo, git)

	result, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source: "claude-code",
		Tool:   "Write",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected capture to succeed")
	}

	// Verify truncation by reading the action back.
	// The action's diff should be at most 100KB + "[truncated]" + newline.
	db := repo.Client().DB()
	var storedDiff string
	err = db.QueryRowContext(context.Background(),
		`SELECT diff FROM actions WHERE id = ?`, result.ActionID,
	).Scan(&storedDiff)
	if err != nil {
		t.Fatalf("query action diff: %v", err)
	}
	if len(storedDiff) > 100*1024+len("\n[truncated]")+10 {
		t.Errorf("expected diff to be truncated to ~100KB, got %d bytes", len(storedDiff))
	}
	if !strings.HasSuffix(storedDiff, "[truncated]") {
		t.Error("expected truncated diff to end with [truncated]")
	}
}
