package application_test

import (
	"context"
	"sort"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/project"
)

// TestDirectoryBucketer_GroupsByTopLevelDir asserts that the heuristic planner
// buckets files by the first path component and assigns each bucket to the
// matching configured scope. This is the REQ-008 fallback that fires after the
// LLM planner exhausts its token budget.
func TestDirectoryBucketer_GroupsByTopLevelDir(t *testing.T) {
	files := []string{
		"cmd/commit.go",
		"cmd/init.go",
		"application/commit_service.go",
		"application/scope_service.go",
		"infrastructure/git/client.go",
		"infrastructure/openai/client.go",
	}
	cfg := &project.Config{Scopes: []project.Scope{
		{Name: "cli", Description: "cobra commands in cmd/"},
		{Name: "app", Description: "orchestration services in application/"},
		{Name: "infra", Description: "adapters in infrastructure/"},
	}}

	bucketer := application.NewDirectoryBucketer()
	plan, err := bucketer.Plan(context.Background(), commit.PlanRequest{
		StagedDiff:   &diff.StagedDiff{},
		UnstagedDiff: &diff.StagedDiff{Files: files},
		Config:       cfg,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(plan.Groups); got != 3 {
		t.Fatalf("expected 3 groups, got %d", got)
	}

	// Index groups by their leading file's top-level dir so order does not
	// fix the assertion.
	groupsByDir := make(map[string]commit.CommitGroup, 3)
	for _, g := range plan.Groups {
		if len(g.Files) == 0 {
			t.Fatalf("group has no files: %+v", g)
		}
		dir := topLevelDir(g.Files[0])
		groupsByDir[dir] = g
	}

	checks := []struct {
		dir   string
		scope string
		files []string
	}{
		{"cmd", "cli", []string{"cmd/commit.go", "cmd/init.go"}},
		{"application", "app", []string{"application/commit_service.go", "application/scope_service.go"}},
		{"infrastructure", "infra", []string{"infrastructure/git/client.go", "infrastructure/openai/client.go"}},
	}
	for _, c := range checks {
		g, ok := groupsByDir[c.dir]
		if !ok {
			t.Errorf("missing group for dir %q", c.dir)
			continue
		}
		got := append([]string(nil), g.Files...)
		sort.Strings(got)
		want := append([]string(nil), c.files...)
		sort.Strings(want)
		if !stringSliceEqual(got, want) {
			t.Errorf("dir %q: expected files %v, got %v", c.dir, want, got)
		}
		title := g.Message.Title
		if title == "" {
			t.Errorf("dir %q: empty title", c.dir)
			continue
		}
		// Scoped placeholder shape: "chore(<scope>): ..."
		wantPrefix := "chore(" + c.scope + ")"
		if title[:len(wantPrefix)] != wantPrefix {
			t.Errorf("dir %q: expected title to start with %q, got %q", c.dir, wantPrefix, title)
		}
	}
}

func topLevelDir(p string) string {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return p
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
