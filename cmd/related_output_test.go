package cmd

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestFormatImpactLine_DirectHasNoMarker(t *testing.T) {
	line := formatImpactLine(graph.ImpactEntry{Path: "a.go", CouplingStrength: 0.5, CouplingCount: 4, Depth: 1}, 5, 1)
	if strings.Contains(line, "indirect") || strings.Contains(line, "depth") {
		t.Errorf("direct coupling should carry no depth marker: %q", line)
	}
	if !strings.Contains(line, "50%") || !strings.Contains(line, "(4 co-changes)") {
		t.Errorf("line missing strength/count: %q", line)
	}
}

func TestFormatImpactLine_IndirectIsMarked(t *testing.T) {
	line := formatImpactLine(graph.ImpactEntry{Path: "c.go", CouplingStrength: 0.5, CouplingCount: 6, Depth: 2}, 5, 1)
	if !strings.Contains(line, "[indirect, depth 2]") {
		t.Errorf("transitive coupling must be marked with its depth: %q", line)
	}
}

func TestFormatImpactLine_MultiSeedShowsBreadth(t *testing.T) {
	e := graph.ImpactEntry{Path: "session.go", CouplingStrength: 0.5, CouplingCount: 9, SeedMatches: 2, RelatedTo: []string{"auth.go", "login.go"}, Depth: 1}
	line := formatImpactLine(e, 12, 2)
	if !strings.Contains(line, "[2/2 seeds: auth.go, login.go]") {
		t.Errorf("multi-seed line must show seed breadth: %q", line)
	}
}

func TestFormatImpactLine_ManySeedsAreCapped(t *testing.T) {
	related := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"}
	e := graph.ImpactEntry{Path: "x.go", CouplingStrength: 0.5, CouplingCount: 9, SeedMatches: 6, RelatedTo: related, Depth: 1}
	line := formatImpactLine(e, 4, 104)
	if !strings.Contains(line, "[6/104 seeds:") {
		t.Errorf("must show breadth count out of total: %q", line)
	}
	// The related list must be bounded, not dump all 6 — and never all 104.
	if strings.Contains(line, "e.go") || strings.Contains(line, "f.go") {
		t.Errorf("related-seed list must be capped, got full list: %q", line)
	}
	if !strings.Contains(line, "+2") {
		t.Errorf("capped list must indicate how many more (+2): %q", line)
	}
}

func TestFilterToTests(t *testing.T) {
	result := &graph.ImpactResult{
		Targets: []string{"auth/token.go"},
		CoChanged: []graph.ImpactEntry{
			{Path: "auth/middleware.go"},
			{Path: "auth/token_test.go"},
			{Path: "auth/handler.spec.ts"},
			{Path: "auth/config.go"},
		},
		TotalFound: 4,
	}
	filterToTests(result)
	if result.TotalFound != 2 || len(result.CoChanged) != 2 {
		t.Fatalf("expected 2 test files after filter, got %d: %+v", len(result.CoChanged), result.CoChanged)
	}
	for _, e := range result.CoChanged {
		if !graph.IsTestFile(e.Path) {
			t.Errorf("non-test file survived --tests filter: %q", e.Path)
		}
	}
}

func TestOutputText_ListsLinkingSubjects(t *testing.T) {
	result := &graph.ImpactResult{
		Targets:    []string{"auth/token.go"},
		TotalFound: 1,
		CoChanged: []graph.ImpactEntry{{
			Path:             "auth/middleware.go",
			CouplingStrength: 0.6,
			CouplingCount:    7,
			Depth:            1,
			LinkingCommits: []graph.CommitRef{
				{Hash: "a1b2c3d4e5", Subject: "feat(auth): add token refresh", Timestamp: 300},
				{Hash: "f6f6f6f6f6", Subject: "fix(auth): guard nil token", Timestamp: 200},
			},
		}},
	}
	var sb strings.Builder
	cmd := relatedCmd
	cmd.SetOut(&sb)
	if err := outputText(cmd, result); err != nil {
		t.Fatalf("outputText: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "feat(auth): add token refresh") {
		t.Errorf("linking subject not shown:\n%s", out)
	}
	if !strings.Contains(out, "a1b2c3d") { // short sha
		t.Errorf("linking short sha not shown:\n%s", out)
	}
}

func TestCapJoin(t *testing.T) {
	if got := capJoin([]string{"a", "b"}, 3); got != "a, b" {
		t.Errorf("under cap = %q, want 'a, b'", got)
	}
	if got := capJoin([]string{"a", "b", "c", "d", "e"}, 3); got != "a, b, c +2" {
		t.Errorf("over cap = %q, want 'a, b, c +2'", got)
	}
}

func TestSummarizeTargets_Bounds(t *testing.T) {
	many := make([]string, 104)
	for i := range many {
		many[i] = "f" + string(rune('0'+i%10)) + ".go"
	}
	got := summarizeTargets(many)
	if !strings.Contains(got, "+") || len(got) > 120 {
		t.Errorf("104 targets must be summarized compactly, got %q", got)
	}
	if s := summarizeTargets([]string{"a.go", "b.go"}); s != "a.go, b.go" {
		t.Errorf("few targets shown in full, got %q", s)
	}
}
