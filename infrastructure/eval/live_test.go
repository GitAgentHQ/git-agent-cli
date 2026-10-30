//go:build jevlive

// Live seam measurement. It is excluded from `go test ./...` by the jevlive
// build tag so the suite never calls a model.
//
//	TYPESAFE_API_KEY=... go test -tags jevlive -v ./infrastructure/eval/
//
// The type and scope seam is measured against the repository's own history: the
// recorded commit title is the only ground truth a repository has, and its
// misses are listed so a reviewer can adjudicate a disagreement instead of
// trusting the number.
package eval_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/domain/commit"
	domainProject "github.com/gitagenthq/git-agent/domain/project"
	"github.com/gitagenthq/git-agent/infrastructure/eval"
	"github.com/gitagenthq/git-agent/infrastructure/typesafe"
)

func liveClient(t *testing.T) *typesafe.Client {
	t.Helper()
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}
	return typesafe.NewClient(key, "", "", 0)
}

// TestLiveTypeScopeSeam measures the type and scope judgment over real commits
// and reports how often it matches the recorded title.
func TestLiveTypeScopeSeam(t *testing.T) {
	repo := os.Getenv("JEV_EVAL_REPO")
	if repo == "" {
		repo = "."
	}
	limit := 12

	samples, err := eval.Collector{Dir: repo}.Collect(context.Background(), limit)
	if err != nil {
		t.Fatalf("collecting samples: %v", err)
	}
	if len(samples) == 0 {
		t.Skip("no samples in the repository")
	}

	scopes := readScopes(t, repo)
	judge := typesafe.NewTypeScopeJudge(liveClient(t))

	var report eval.Report
	report.Seam = "type_scope"

	for _, s := range samples {
		wantType, wantScope, ok := eval.ConventionalPrefix(s.Title)
		if !ok {
			t.Logf("skipping %s: title %q has no conventional prefix", s.SHA[:7], s.Title)
			continue
		}

		changes := make([]commit.FileChange, 0, len(s.Files))
		counts := eval.ParseNumstat(s.Numstat)
		for _, file := range s.Files {
			c := commit.FileChange{Path: file}
			if n, found := counts[file]; found {
				c.Adds, c.Dels = n[0], n[1]
			}
			changes = append(changes, c)
		}

		started := time.Now()
		verdict, judgeErr := judge.JudgeTypeScope(context.Background(), commit.TypeScopeRequest{
			Files: changes, Scopes: scopes,
		})
		elapsed := time.Since(started)
		if judgeErr != nil {
			t.Errorf("judging %s: %v", s.SHA[:7], judgeErr)
			continue
		}
		t.Logf("%s want %s(%s) got %s(%s) type %.2f scope %.2f in %s",
			s.SHA[:7], wantType, wantScope, verdict.Type, verdict.Scope,
			verdict.TypeConfidence, verdict.ScopeConfidence, elapsed.Round(time.Millisecond))
		report.RecordParts(s, wantType, wantScope, verdict.Type, verdict.Scope, verdict.Confidence(),
			verdict.TypeConfidence, verdict.ScopeConfidence, elapsed, estimateTokens(s))
	}
	report.Finish()

	t.Logf("\n%s", report.String())
	if report.Judged == 0 {
		t.Fatal("no sample carried a conventional prefix")
	}

	// The scope part is pinned at its floor, so two things must hold: it agrees
	// most of the time, and every disagreement arrives below the floor. A
	// disagreement above the floor would mean the confidence says nothing.
	const scopeFloor = 0.7
	if report.ScopeHits*100/report.Judged < 80 {
		t.Errorf("scope agreement %d%% is below the level that would justify pinning it",
			report.ScopeHits*100/report.Judged)
	}
	if !report.ScopeFloorHolds(scopeFloor) {
		t.Errorf("a scope disagreement arrived at or above the %.2f floor: %v", scopeFloor, report.ScopeMissConfidences)
	}

	// The type part is not pinned, and this run is the evidence: measurement
	// found its disagreements arrive highly confident, so no floor rescues it.
	// This assertion documents the finding rather than gating it.
	if !report.TypeFloorHolds(0.85) {
		t.Logf("type disagreements arrive at or above 0.85: %v — the type stays with the model",
			report.TypeMissConfidences)
	}
}

// TestLiveScopeSeam measures the scope decision seam against the scopes the
// repository already uses.
func TestLiveScopeSeam(t *testing.T) {
	repo := os.Getenv("JEV_EVAL_REPO")
	if repo == "" {
		repo = "."
	}
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}

	dirs, files := repoLayout(t, repo)
	decider := typesafe.NewScopeDecider(typesafe.NewClient(key, "", "", 0))

	// The realistic path passes the configured scopes, because that is what a
	// repository looks like when the judgment runs.
	scopes := readScopes(t, repo)
	existing := make([]domainProject.Scope, 0, len(scopes))
	for name, description := range scopes {
		if name == "" {
			continue
		}
		existing = append(existing, domainProject.Scope{Name: name, Description: description})
	}

	started := time.Now()
	verdict, err := decider.DecideScopes(context.Background(), domainProject.ScopeDecisionRequest{
		Dirs: dirs, Files: files, ExistingScopes: existing,
	})
	if err != nil {
		t.Fatalf("scope decision failed: %v", err)
	}
	t.Logf("%d directories judged in %s, confidence %.2f", len(verdict.Assignments),
		time.Since(started).Round(time.Millisecond), verdict.Confidence)
	for _, a := range verdict.Assignments {
		t.Logf("  %-24s %-8s %s", a.Dir, a.Action, a.Scope)
	}
	if len(verdict.Assignments) != len(domainProject.CandidateDirs(dirs)) {
		t.Errorf("expected one assignment per candidate directory, got %d of %d",
			len(verdict.Assignments), len(domainProject.CandidateDirs(dirs)))
	}
	if verdict.Confidence < 0.7 {
		t.Errorf("scope confidence %.2f is below the floor the on mode needs, so the seam stays gated", verdict.Confidence)
	}
}

// readScopes reads the repository's own scope names and descriptions, so the
// judgment chooses among the scopes the project actually uses.
func readScopes(t *testing.T, repo string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(repo + "/.git-agent/config.yml")
	if err != nil {
		t.Logf("no project config found, measuring with a single scope: %v", err)
		return map[string]string{"": "No scope description is configured."}
	}
	out := map[string]string{}
	var name, description string
	inScopes := false
	flush := func() {
		if name != "" {
			out[name] = description
		}
		name, description = "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "scopes:"):
			inScopes = true
		case inScopes && strings.HasPrefix(trimmed, "hook"):
			inScopes = false
		case inScopes && strings.HasPrefix(trimmed, "- name:"):
			flush()
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
		case inScopes && strings.HasPrefix(trimmed, "description:"):
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		}
	}
	flush()
	if len(out) == 0 {
		return map[string]string{"": "No scope description is configured."}
	}
	return out
}

func repoLayout(t *testing.T, repo string) (dirs, files []string) {
	t.Helper()
	var err error
	dirs, files, err = repoLayoutOf(repo)
	if err != nil {
		t.Fatalf("reading the repository layout: %v", err)
	}
	return dirs, files
}

// repoLayoutOf reads the whole tracked file list of a repository, which is the
// evidence both the classifier and the scope judgment receive.
func repoLayoutOf(repo string) (dirs, files []string, err error) {
	samples, err := eval.Collector{Dir: repo}.Collect(context.Background(), 1)
	if err != nil {
		return nil, nil, err
	}
	if len(samples) == 0 {
		return nil, nil, fmt.Errorf("no commits to read the layout from")
	}
	files = samples[0].Files
	return topLevelDirs(files), files, nil
}

func topLevelDirs(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, file := range files {
		dir, _, found := strings.Cut(file, "/")
		if !found || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// estimateTokens approximates the state size a judgment sends, which is the cost
// figure the report needs.
func estimateTokens(s eval.Sample) int {
	total := len(s.Numstat)
	for _, file := range s.Files {
		total += len(file) + 8
	}
	return total / 4
}
