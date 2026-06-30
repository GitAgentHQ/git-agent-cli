package application_test

import (
	"context"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
)

// seedLinkedCommits creates two files (token.go, middleware.go) co-changed
// across three commits with distinct subjects and ascending timestamps, plus a
// noise commit touching only token.go. It returns after recomputing co_changed.
func seedLinkedCommits(t *testing.T, repo graph.GraphRepository) {
	t.Helper()
	ctx := context.Background()
	for _, f := range []string{"auth/token.go", "auth/middleware.go"} {
		if err := repo.UpsertFile(ctx, graph.FileNode{Path: f}); err != nil {
			t.Fatalf("upsert file: %v", err)
		}
	}
	type c struct {
		hash    string
		subject string
		ts      int64
		files   []string
	}
	commits := []c{
		{"c1", "feat(auth): add token store", 100, []string{"auth/token.go", "auth/middleware.go"}},
		{"c2", "fix(auth): guard nil token\n\nbody line", 200, []string{"auth/token.go", "auth/middleware.go"}},
		{"c3", "refactor(auth): token refresh", 300, []string{"auth/token.go", "auth/middleware.go"}},
		{"c4", "docs: unrelated", 400, []string{"auth/token.go"}},
	}
	for _, cm := range commits {
		if err := repo.UpsertCommit(ctx, graph.CommitNode{Hash: cm.hash, Message: cm.subject, Timestamp: cm.ts}); err != nil {
			t.Fatalf("upsert commit: %v", err)
		}
		for _, f := range cm.files {
			if err := repo.CreateModifies(ctx, graph.ModifiesEdge{CommitHash: cm.hash, FilePath: f}); err != nil {
				t.Fatalf("create modifies: %v", err)
			}
		}
	}
	if err := repo.RecomputeCoChanged(ctx, 3, 50); err != nil {
		t.Fatalf("recompute co_changed: %v", err)
	}
}

func TestLinkingCommits_BindsThePair(t *testing.T) {
	repo, cleanup := setupGraphDB(t)
	defer cleanup()
	seedLinkedCommits(t, repo)

	got, err := repo.LinkingCommits(context.Background(), "auth/token.go", "auth/middleware.go", 3)
	if err != nil {
		t.Fatalf("LinkingCommits: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 linking commits, got %d: %+v", len(got), got)
	}
	// Most-recent first.
	if got[0].Hash != "c3" || got[1].Hash != "c2" || got[2].Hash != "c1" {
		t.Errorf("wrong order: %+v", got)
	}
	// Subject is the first line of the message (c2 has a body).
	if got[1].Subject != "fix(auth): guard nil token" {
		t.Errorf("subject not first-line trimmed: %q", got[1].Subject)
	}
	// The noise commit c4 (token.go only) must not appear.
	for _, c := range got {
		if c.Hash == "c4" {
			t.Errorf("c4 touched only token.go and must not link the pair")
		}
	}
}

func TestImpactService_EnrichesWithLinkingCommits(t *testing.T) {
	repo, cleanup := setupGraphDB(t)
	defer cleanup()
	seedLinkedCommits(t, repo)

	svc := application.NewImpactService(repo)
	ctx := context.Background()

	// Without IncludeCommits: entries carry no evidence.
	plain, err := svc.Impact(ctx, graph.ImpactRequest{Paths: []string{"auth/token.go"}, IncludeCommits: false})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	mid := findEntry(t, plain, "auth/middleware.go")
	if len(mid.LinkingCommits) != 0 {
		t.Errorf("IncludeCommits=false should not enrich, got %+v", mid.LinkingCommits)
	}

	// With IncludeCommits: middleware.go carries the linking commit subjects.
	enriched, err := svc.Impact(ctx, graph.ImpactRequest{Paths: []string{"auth/token.go"}, IncludeCommits: true})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	mid = findEntry(t, enriched, "auth/middleware.go")
	if len(mid.LinkingCommits) == 0 {
		t.Fatalf("IncludeCommits=true should attach linking commits")
	}
	if mid.LinkingCommits[0].Hash != "c3" {
		t.Errorf("expected most-recent linking commit c3 first, got %q", mid.LinkingCommits[0].Hash)
	}
	if mid.LinkingCommits[0].Subject == "" {
		t.Errorf("linking commit subject should be populated")
	}
}

func findEntry(t *testing.T, result *graph.ImpactResult, path string) graph.ImpactEntry {
	t.Helper()
	if result == nil {
		t.Fatalf("nil result")
	}
	for _, e := range result.CoChanged {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("entry %q not found in result %+v", path, result.CoChanged)
	return graph.ImpactEntry{}
}
