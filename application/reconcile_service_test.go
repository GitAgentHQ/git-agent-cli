package application_test

import (
	"context"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infragraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// reconcileFakeGit drives the residual-detection flow: DiffNameOnly reports the
// working-tree candidate set, HashObject reports the current blob per file.
type reconcileFakeGit struct {
	graph.GraphGitClient
	changed []string
	current map[string]string
}

func (g *reconcileFakeGit) DiffNameOnly(_ context.Context) ([]string, error) {
	return g.changed, nil
}

func (g *reconcileFakeGit) HashObject(_ context.Context, filePath string) (string, error) {
	if h, ok := g.current[filePath]; ok {
		return h, nil
	}
	return "", nil
}

// outOfBandEvents returns the appended Out-of-Band Events in seq order.
func outOfBandEvents(t *testing.T, repo *infragraph.SQLiteRepository) []graph.EventRecord {
	t.Helper()
	cur, err := repo.StreamEvents(context.Background(), 0)
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	defer cur.Close()
	var out []graph.EventRecord
	for cur.Next() {
		e := cur.Event()
		if e.Kind == graph.EventKindOutOfBand {
			out = append(out, e)
		}
	}
	if err := cur.Err(); err != nil {
		t.Fatalf("cursor err: %v", err)
	}
	return out
}

// eventFilesForSeq returns the (before_blob, after_blob) for an event_files row.
func eventFileBlobs(t *testing.T, repo *infragraph.SQLiteRepository, seq int64, path string) (string, string) {
	t.Helper()
	var before, after string
	err := repo.Client().DB().QueryRowContext(context.Background(),
		`SELECT COALESCE(before_blob,''), COALESCE(after_blob,'') FROM event_files WHERE event_seq = ? AND file_path = ?`,
		seq, path,
	).Scan(&before, &after)
	if err != nil {
		t.Fatalf("event_files for seq=%d path=%s: %v", seq, path, err)
	}
	return before, after
}

func TestReconcileService_AppendsUnknownOutOfBandEvent(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	// Seed the log: an Edit Event for a.go whose last-known-after blob the cold
	// path will have recorded. Build the projections so event_files has the
	// before/after blobs.
	const lastKnownAfter = "blob-a-v1"
	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")
	pr := application.NewProjectionRebuilder(repo, &hashObjectFakeGit{
		blobs: map[string]string{"a.go": lastKnownAfter},
	})
	if err := pr.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// a.go changed out of band: current blob differs from last-known-after, and
	// there is no corresponding capture Event for that change.
	gitFake := &reconcileFakeGit{
		changed: []string{"a.go"},
		current: map[string]string{"a.go": "blob-a-v2"},
	}
	svc := application.NewReconcileService(repo, gitFake)
	res, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.OutOfBandAppended != 1 {
		t.Fatalf("OutOfBandAppended = %d, want 1", res.OutOfBandAppended)
	}

	oob := outOfBandEvents(t, repo)
	if len(oob) != 1 {
		t.Fatalf("out-of-band events = %d, want 1", len(oob))
	}
	e := oob[0]
	if e.Source != graph.EventSourceUnknown {
		t.Errorf("Source = %q, want unknown", e.Source)
	}
	if e.Kind != graph.EventKindOutOfBand {
		t.Errorf("Kind = %q, want out-of-band", e.Kind)
	}
	if e.ToolName != "external-edit" {
		t.Errorf("ToolName = %q, want external-edit", e.ToolName)
	}

	before, after := eventFileBlobs(t, repo, e.Seq, "a.go")
	if before != lastKnownAfter {
		t.Errorf("before_blob = %q, want last-known-after %q", before, lastKnownAfter)
	}
	if after != "blob-a-v2" {
		t.Errorf("after_blob = %q, want current %q", after, "blob-a-v2")
	}

	// It was chained into the same log: a clean verify proves a valid
	// prev_hash/this_hash linkage, not a forged observed Event.
	vr, err := repo.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if vr.Status != "ok" {
		t.Errorf("VerifyChain status = %q, want ok", vr.Status)
	}
}

func TestReconcileService_OnlyUnexplainedResidual(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	// a.go was changed by a captured Edit Event; its current working-tree hash
	// matches the state implied by Replay (the recorded after_blob).
	const aBlob = "blob-a-current"
	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")
	pr := application.NewProjectionRebuilder(repo, &hashObjectFakeGit{
		blobs: map[string]string{"a.go": aBlob},
	})
	if err := pr.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// DiffNameOnly reports both a.go (explained, current == recorded) and b.go
	// (no Event, unexplained residual).
	gitFake := &reconcileFakeGit{
		changed: []string{"a.go", "b.go"},
		current: map[string]string{"a.go": aBlob, "b.go": "blob-b-current"},
	}
	svc := application.NewReconcileService(repo, gitFake)
	res, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.OutOfBandAppended != 1 {
		t.Fatalf("OutOfBandAppended = %d, want 1 (only b.go)", res.OutOfBandAppended)
	}

	oob := outOfBandEvents(t, repo)
	if len(oob) != 1 {
		t.Fatalf("out-of-band events = %d, want 1", len(oob))
	}

	// The single residual Event must be for b.go, not a.go.
	var path string
	if err := repo.Client().DB().QueryRowContext(ctx,
		`SELECT file_path FROM event_files WHERE event_seq = ?`, oob[0].Seq,
	).Scan(&path); err != nil {
		t.Fatalf("event_files path: %v", err)
	}
	if path != "b.go" {
		t.Errorf("residual Event is for %q, want b.go (a.go is explained)", path)
	}
}
