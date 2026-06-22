package application

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestParseCommitHashPrefersFullHashLine(t *testing.T) {
	full := "1234567890abcdef1234567890abcdef12345678"
	got := parseCommitHash("[main abc1234] subject\ncommit " + full)

	if got != full {
		t.Fatalf("parseCommitHash() = %q, want full hash %q", got, full)
	}
}

func TestGraphActionLinker_LinksActionToEachSplitCommit(t *testing.T) {
	repoDir, _ := testRepo(t)
	repo := openTestDB(t, repoDir)
	ctx := context.Background()

	if err := repo.UpsertSession(ctx, graph.SessionNode{
		ID:        "session-1",
		Source:    "claude-code",
		StartedAt: 100,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if err := repo.CreateActionBatch(ctx, graph.ActionNode{
		ID:           "session-1:1",
		SessionID:    "session-1",
		Sequence:     1,
		Tool:         "Edit",
		FilesChanged: []string{"a.go", "b.go"},
		Timestamp:    timeNowForTest(),
	}, []graph.FileChange{
		{Path: "a.go", Additions: 1},
		{Path: "b.go", Additions: 1},
	}); err != nil {
		t.Fatalf("CreateActionBatch() error = %v", err)
	}

	linker := NewGraphActionLinker(repo)
	if err := linker.LinkActionsToCommit(ctx, "commit 1111111111111111111111111111111111111111", []string{"a.go"}); err != nil {
		t.Fatalf("first LinkActionsToCommit() error = %v", err)
	}
	if err := linker.LinkActionsToCommit(ctx, "commit 2222222222222222222222222222222222222222", []string{"b.go"}); err != nil {
		t.Fatalf("second LinkActionsToCommit() error = %v", err)
	}

	rows, err := repo.Client().DB().QueryContext(ctx,
		`SELECT commit_hash, file_path FROM action_produces WHERE action_id = ? ORDER BY commit_hash, file_path`,
		"session-1:1",
	)
	if err != nil {
		t.Fatalf("query action_produces: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var commitHash, filePath string
		if err := rows.Scan(&commitHash, &filePath); err != nil {
			t.Fatalf("scan action_produces: %v", err)
		}
		got[commitHash] = filePath
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate action_produces: %v", err)
	}

	want := map[string]string{
		"1111111111111111111111111111111111111111": "a.go",
		"2222222222222222222222222222222222222222": "b.go",
	}
	if len(got) != len(want) {
		t.Fatalf("action should link to both split commits, got rows %v", got)
	}
	for commitHash, filePath := range want {
		if got[commitHash] != filePath {
			t.Fatalf("action_produces[%s] = %q, want %q (all rows %v)", commitHash, got[commitHash], filePath, got)
		}
	}
}

func TestGraphActionLinker_DoesNotRelinkAfterReindexPreservesProducesRows(t *testing.T) {
	repoDir, _ := testRepo(t)
	repo := openTestDB(t, repoDir)
	ctx := context.Background()

	if err := repo.UpsertSession(ctx, graph.SessionNode{ID: "session-1", Source: "claude-code", StartedAt: 100}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if err := repo.CreateActionBatch(ctx, graph.ActionNode{
		ID:           "session-1:1",
		SessionID:    "session-1",
		Sequence:     1,
		Tool:         "Edit",
		FilesChanged: []string{"a.go"},
		Timestamp:    timeNowForTest(),
	}, []graph.FileChange{{Path: "a.go", Additions: 1}}); err != nil {
		t.Fatalf("CreateActionBatch() error = %v", err)
	}

	if err := repo.CreateActionProduces(ctx, "session-1:1", "1111111111111111111111111111111111111111", "a.go"); err != nil {
		t.Fatalf("CreateActionProduces() error = %v", err)
	}
	if err := repo.ResetIndexData(ctx); err != nil {
		t.Fatalf("ResetIndexData() error = %v", err)
	}
	if err := NewGraphActionLinker(repo).LinkActionsToCommit(ctx, "commit 2222222222222222222222222222222222222222", []string{"a.go"}); err != nil {
		t.Fatalf("LinkActionsToCommit() error = %v", err)
	}

	var newLink string
	err := repo.Client().DB().QueryRowContext(ctx,
		`SELECT commit_hash FROM action_produces WHERE action_id = ? AND commit_hash = ?`,
		"session-1:1", "2222222222222222222222222222222222222222",
	).Scan(&newLink)
	if err != sql.ErrNoRows {
		t.Fatalf("old linked action should not relink after reset, got commit %q err %v", newLink, err)
	}
}

func timeNowForTest() int64 {
	return 4102444800
}

func TestParseCommitHashFallsBackToGitOutputShortHash(t *testing.T) {
	got := parseCommitHash("[main abc1234] subject")
	if got != "abc1234" {
		t.Fatalf("parseCommitHash() fallback = %q", got)
	}
	if strings.TrimSpace(parseCommitHash("nothing useful")) != "" {
		t.Fatal("parseCommitHash() should return empty for unparseable output")
	}
}
