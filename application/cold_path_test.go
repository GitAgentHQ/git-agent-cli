package application_test

import (
	"context"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestSyncEventLog_RebuildThenReconcileThenRebuild(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	const lastKnownAfter = "blob-a-v1"
	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")

	gitFake := &seqHashGit{
		reconcile: &reconcileFakeGit{
			changed: []string{"a.go"},
			current: map[string]string{"a.go": "blob-a-v2"},
		},
		rebuild: &hashObjectFakeGit{blobs: map[string]string{"a.go": lastKnownAfter}},
	}

	res, err := application.SyncEventLog(ctx, repo, gitFake)
	if err != nil {
		t.Fatalf("SyncEventLog: %v", err)
	}
	if res.OutOfBandAppended != 1 {
		t.Fatalf("OutOfBandAppended = %d, want 1", res.OutOfBandAppended)
	}

	oob := outOfBandEvents(t, repo)
	if len(oob) != 1 {
		t.Fatalf("out-of-band events = %d, want 1", len(oob))
	}

	before, after := eventFileBlobs(t, repo, oob[0].Seq, "a.go")
	if before != lastKnownAfter {
		t.Errorf("before_blob = %q, want %q", before, lastKnownAfter)
	}
	if after != "blob-a-v2" {
		t.Errorf("after_blob = %q, want blob-a-v2", after)
	}

	if countRows(t, repo, "actions") != 2 {
		t.Errorf("actions = %d, want 2 (tool + out-of-band)", countRows(t, repo, "actions"))
	}
}

func TestSyncEventLog_SkipsSecondRebuildWhenNothingAppended(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	const aBlob = "blob-a-current"
	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")

	gitFake := &seqHashGit{
		reconcile: &reconcileFakeGit{
			changed: []string{"a.go"},
			current: map[string]string{"a.go": aBlob},
		},
		rebuild: &hashObjectFakeGit{blobs: map[string]string{"a.go": aBlob}},
	}

	res, err := application.SyncEventLog(ctx, repo, gitFake)
	if err != nil {
		t.Fatalf("SyncEventLog: %v", err)
	}
	if res.OutOfBandAppended != 0 {
		t.Fatalf("OutOfBandAppended = %d, want 0", res.OutOfBandAppended)
	}
	if countRows(t, repo, "actions") != 1 {
		t.Errorf("actions = %d, want 1", countRows(t, repo, "actions"))
	}
}

// seqHashGit routes HashObject by call order: first and third passes are Rebuild
// (stable rebuild blobs); the second pass is Reconcile (current working-tree blobs).
type seqHashGit struct {
	graph.GraphGitClient
	reconcile *reconcileFakeGit
	rebuild   *hashObjectFakeGit
	hashCalls int
}

func (g *seqHashGit) DiffNameOnly(ctx context.Context) ([]string, error) {
	return g.reconcile.DiffNameOnly(ctx)
}

func (g *seqHashGit) HashObject(ctx context.Context, path string) (string, error) {
	g.hashCalls++
	if g.hashCalls == 2 {
		return g.reconcile.HashObject(ctx, path)
	}
	return g.rebuild.HashObject(ctx, path)
}
