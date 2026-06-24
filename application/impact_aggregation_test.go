package application

import (
	"context"
	"reflect"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestImpact_MultiSeed_BoostsSharedNeighbor(t *testing.T) {
	repo := setupImpactTest(t)
	// session.go co-changes with BOTH seeds; lonely.go with only one (but stronger).
	seedCoChanged(t, repo, "auth.go", "session.go", 5, 0.5)
	seedCoChanged(t, repo, "login.go", "session.go", 5, 0.5)
	seedCoChanged(t, repo, "auth.go", "lonely.go", 5, 0.9)

	res, err := NewImpactService(repo).Impact(context.Background(), graph.ImpactRequest{
		Paths: []string{"auth.go", "login.go"},
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}

	if !reflect.DeepEqual(res.Targets, []string{"auth.go", "login.go"}) {
		t.Errorf("Targets = %v, want [auth.go login.go]", res.Targets)
	}
	if len(res.CoChanged) != 2 {
		t.Fatalf("CoChanged = %d entries, want 2: %+v", len(res.CoChanged), res.CoChanged)
	}
	// session.go (seed_matches 2, score 1.0) must outrank lonely.go (seed_matches 1, score 0.9).
	first := res.CoChanged[0]
	if first.Path != "session.go" || first.SeedMatches != 2 {
		t.Errorf("first = %q seed_matches=%d, want session.go matches=2", first.Path, first.SeedMatches)
	}
	if first.Score < 0.99 {
		t.Errorf("session.go score = %f, want ~1.0 (sum of 0.5+0.5)", first.Score)
	}
	second := res.CoChanged[1]
	if second.Path != "lonely.go" || second.SeedMatches != 1 {
		t.Errorf("second = %q seed_matches=%d, want lonely.go matches=1", second.Path, second.SeedMatches)
	}
}

func TestImpact_MultiSeed_RelatedToAndScore(t *testing.T) {
	repo := setupImpactTest(t)
	seedCoChanged(t, repo, "auth.go", "session.go", 5, 0.5)
	seedCoChanged(t, repo, "login.go", "session.go", 4, 0.4)

	res, err := NewImpactService(repo).Impact(context.Background(), graph.ImpactRequest{
		Paths: []string{"auth.go", "login.go"},
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(res.CoChanged) != 1 {
		t.Fatalf("want 1 entry, got %+v", res.CoChanged)
	}
	e := res.CoChanged[0]
	if !reflect.DeepEqual(e.RelatedTo, []string{"auth.go", "login.go"}) {
		t.Errorf("RelatedTo = %v, want [auth.go login.go]", e.RelatedTo)
	}
	if e.Score < 0.89 || e.Score > 0.91 {
		t.Errorf("Score = %f, want ~0.9 (0.5+0.4)", e.Score)
	}
	if e.CouplingCount != 9 {
		t.Errorf("CouplingCount = %d, want 9 (5+4 across seeds)", e.CouplingCount)
	}
}

func TestImpact_MultiSeed_ExcludesSeedsFromResults(t *testing.T) {
	repo := setupImpactTest(t)
	seedCoChanged(t, repo, "a.go", "b.go", 5, 0.7)

	res, err := NewImpactService(repo).Impact(context.Background(), graph.ImpactRequest{
		Paths: []string{"a.go", "b.go"},
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	for _, e := range res.CoChanged {
		if e.Path == "a.go" || e.Path == "b.go" {
			t.Errorf("seed %q must not appear in results", e.Path)
		}
	}
}
