package application_test

import (
	"context"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/domain/project"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"os"
	"path/filepath"
)

// --- helpers for graph tests ---

func setupGraphDB(t *testing.T) (graph.GraphRepository, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph.db")
	client := infraGraph.NewSQLiteClient(dbPath)
	repo := infraGraph.NewSQLiteRepository(client)
	ctx := context.Background()

	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return repo, func() {
		repo.Close()
		os.Remove(dbPath)
	}
}

func seedCoChanged(t *testing.T, repo graph.GraphRepository) {
	t.Helper()
	ctx := context.Background()

	// Create files.
	for _, f := range []string{"a.go", "b.go", "c.go", "d.go", "unrelated.go"} {
		if err := repo.UpsertFile(ctx, graph.FileNode{Path: f}); err != nil {
			t.Fatalf("upsert file: %v", err)
		}
	}

	// Create commits where a.go and b.go change together, and b.go and c.go change together.
	for i := 0; i < 5; i++ {
		hash := "abc" + string(rune('0'+i))
		if err := repo.UpsertCommit(ctx, graph.CommitNode{Hash: hash, Message: "test"}); err != nil {
			t.Fatalf("upsert commit: %v", err)
		}
		if err := repo.CreateModifies(ctx, graph.ModifiesEdge{CommitHash: hash, FilePath: "a.go"}); err != nil {
			t.Fatalf("create modifies: %v", err)
		}
		if err := repo.CreateModifies(ctx, graph.ModifiesEdge{CommitHash: hash, FilePath: "b.go"}); err != nil {
			t.Fatalf("create modifies: %v", err)
		}
	}
	for i := 0; i < 4; i++ {
		hash := "bcd" + string(rune('0'+i))
		if err := repo.UpsertCommit(ctx, graph.CommitNode{Hash: hash, Message: "test"}); err != nil {
			t.Fatalf("upsert commit: %v", err)
		}
		if err := repo.CreateModifies(ctx, graph.ModifiesEdge{CommitHash: hash, FilePath: "b.go"}); err != nil {
			t.Fatalf("create modifies: %v", err)
		}
		if err := repo.CreateModifies(ctx, graph.ModifiesEdge{CommitHash: hash, FilePath: "c.go"}); err != nil {
			t.Fatalf("create modifies: %v", err)
		}
	}

	// Recompute co-change data.
	if err := repo.RecomputeCoChanged(ctx, 3, 50); err != nil {
		t.Fatalf("recompute co_changed: %v", err)
	}
}

func TestGraphCoChangeProvider_GetHintsForFiles(t *testing.T) {
	repo, cleanup := setupGraphDB(t)
	defer cleanup()
	seedCoChanged(t, repo)

	provider := application.NewGraphCoChangeProvider(repo)
	ctx := context.Background()

	// Query with a.go, b.go, c.go - should get pairs where both files are in the list.
	hints, err := provider.GetHintsForFiles(ctx, []string{"a.go", "b.go", "c.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hints) == 0 {
		t.Fatal("expected at least one co-change hint, got none")
	}

	// Verify all returned hints have both files in our query list.
	querySet := map[string]bool{"a.go": true, "b.go": true, "c.go": true}
	for _, h := range hints {
		if !querySet[h.FileA] || !querySet[h.FileB] {
			t.Errorf("hint contains file outside query set: %s <-> %s", h.FileA, h.FileB)
		}
		if h.Strength <= 0 || h.Strength > 1 {
			t.Errorf("unexpected strength value: %f", h.Strength)
		}
	}

	// Verify hints are sorted by strength descending.
	for i := 1; i < len(hints); i++ {
		if hints[i].Strength > hints[i-1].Strength {
			t.Errorf("hints not sorted by strength descending at index %d: %f > %f",
				i, hints[i].Strength, hints[i-1].Strength)
		}
	}
}

func TestGraphCoChangeProvider_NoPairs(t *testing.T) {
	repo, cleanup := setupGraphDB(t)
	defer cleanup()
	seedCoChanged(t, repo)

	provider := application.NewGraphCoChangeProvider(repo)
	ctx := context.Background()

	// Query with d.go only - no co-change relationships with other queried files.
	hints, err := provider.GetHintsForFiles(ctx, []string{"d.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hints) != 0 {
		t.Errorf("expected empty hints for single file with no co-change, got %d", len(hints))
	}
}

func TestGraphCoChangeProvider_OnlyInListPairs(t *testing.T) {
	repo, cleanup := setupGraphDB(t)
	defer cleanup()
	seedCoChanged(t, repo)

	provider := application.NewGraphCoChangeProvider(repo)
	ctx := context.Background()

	// Query with a.go only (b.go is co-changed with a.go, but b.go is not in the list).
	hints, err := provider.GetHintsForFiles(ctx, []string{"a.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hints) != 0 {
		t.Errorf("expected no hints when co-changed partner is not in query list, got %d", len(hints))
	}
}

func TestNilCoChangeProvider_GracefulDegradation(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"main.go"})}

	// Pass no coChange provider (variadic empty) - should work without error.
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error with nil coChange provider: %v", err)
	}
	if len(result.Commits) == 0 {
		t.Error("expected at least one commit result")
	}
}

// mockCoChangeProvider is a test double for CoChangeProvider.
type mockCoChangeProvider struct {
	hints []commit.CoChangeHint
	err   error
}

func (m *mockCoChangeProvider) GetHintsForFiles(_ context.Context, _ []string) ([]commit.CoChangeHint, error) {
	return m.hints, m.err
}

func TestCommitService_WithCoChangeHints_PassesToPlanner(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			{Files: []string{}, Content: "", Lines: 0},
			{Files: []string{"a.go", "b.go"}, Content: "+a+b", Lines: 2},
		},
		stagedDiff: &diff.StagedDiff{Files: []string{"a.go"}, Content: "+a", Lines: 1},
	}

	// Recording planner to verify CoChangeHints are passed.
	rp := &recordingPlanner{}
	rp.plan = &commit.CommitPlan{Groups: []commit.CommitGroup{
		{Files: []string{"a.go", "b.go"}},
	}}

	coProvider := &mockCoChangeProvider{
		hints: []commit.CoChangeHint{
			{FileA: "a.go", FileB: "b.go", Strength: 0.85},
		},
	}

	svc := application.NewCommitService(gen, rp, git, noopHook(), nil, nil, nil, coProvider)

	req := application.CommitRequest{Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rp.reqs) == 0 {
		t.Fatal("expected at least one Plan call")
	}
	if len(rp.reqs[0].CoChangeHints) != 1 {
		t.Fatalf("expected 1 co-change hint passed to planner, got %d", len(rp.reqs[0].CoChangeHints))
	}
	if rp.reqs[0].CoChangeHints[0].FileA != "a.go" || rp.reqs[0].CoChangeHints[0].FileB != "b.go" {
		t.Errorf("unexpected hint files: %s <-> %s", rp.reqs[0].CoChangeHints[0].FileA, rp.reqs[0].CoChangeHints[0].FileB)
	}
}

func TestCommitService_CoChangeError_GracefulDegradation(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			{Files: []string{}, Content: "", Lines: 0},
			{Files: []string{"a.go", "b.go"}, Content: "+a+b", Lines: 2},
		},
		stagedDiff: &diff.StagedDiff{Files: []string{"a.go"}, Content: "+a", Lines: 1},
	}

	planner := &mockCommitPlanner{plan: &commit.CommitPlan{Groups: []commit.CommitGroup{
		{Files: []string{"a.go", "b.go"}},
	}}}

	coProvider := &mockCoChangeProvider{
		err: context.DeadlineExceeded, // simulate a failure
	}

	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, coProvider)

	req := application.CommitRequest{Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("expected graceful degradation on co-change error, got: %v", err)
	}
	if len(result.Commits) == 0 {
		t.Error("expected at least one commit result")
	}
}

// recordingPlanner captures Plan requests for inspection.
type recordingPlanner struct {
	reqs []*commit.PlanRequest
	plan *commit.CommitPlan
}

func (r *recordingPlanner) Plan(_ context.Context, req commit.PlanRequest) (*commit.CommitPlan, error) {
	r.reqs = append(r.reqs, &req)
	return r.plan, nil
}
