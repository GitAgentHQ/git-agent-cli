package graph

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func newBatchTestRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	client := NewSQLiteClient(filepath.Join(t.TempDir(), "test.db"))
	ctx := context.Background()
	if err := client.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return NewSQLiteRepository(client)
}

func countCommits(t *testing.T, r *SQLiteRepository) int {
	t.Helper()
	var n int
	if err := r.db().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM commits").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestRunInTx_CommitsAllWrites(t *testing.T) {
	repo := newBatchTestRepo(t)
	ctx := context.Background()
	err := repo.RunInTx(ctx, func() error {
		for i := 0; i < 3; i++ {
			if err := repo.UpsertCommit(ctx, graph.CommitNode{Hash: string(rune('a' + i))}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInTx: %v", err)
	}
	if got := countCommits(t, repo); got != 3 {
		t.Errorf("committed commits = %d, want 3", got)
	}
}

func TestRunInTx_RollsBackOnError(t *testing.T) {
	repo := newBatchTestRepo(t)
	ctx := context.Background()
	sentinel := errors.New("boom")
	err := repo.RunInTx(ctx, func() error {
		if err := repo.UpsertCommit(ctx, graph.CommitNode{Hash: "a"}); err != nil {
			return err
		}
		return sentinel // abort: the prior write must not persist
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunInTx error = %v, want sentinel", err)
	}
	if got := countCommits(t, repo); got != 0 {
		t.Errorf("after rollback commits = %d, want 0", got)
	}
}

func TestRunInTx_WritesVisibleAfterCommit_AutocommitStillWorks(t *testing.T) {
	repo := newBatchTestRepo(t)
	ctx := context.Background()
	// Outside any batch, a single upsert autocommits as before.
	if err := repo.UpsertCommit(ctx, graph.CommitNode{Hash: "solo"}); err != nil {
		t.Fatalf("autocommit upsert: %v", err)
	}
	if got := countCommits(t, repo); got != 1 {
		t.Errorf("autocommit commits = %d, want 1", got)
	}
}
