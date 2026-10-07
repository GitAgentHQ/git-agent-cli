//go:build jevlive

// A/B measurement: does the System One layer produce better decisions than the
// generative path it would replace?
//
// Both sides answer the same question on the same samples, drawn from real
// repository history, and both are scored against the commit title that was
// actually recorded. Three comparisons come out of one run:
//
//   - the commit type, baseline generator versus judgment
//   - the commit scope, baseline generator versus judgment
//   - the file grouping, baseline planner versus judgment
//
// The grouping has no single correct answer, so it is reported as agreement plus
// the list of disagreements for a human to adjudicate.
//
//	TYPESAFE_API_KEY=... go test -tags jevlive -v -run TestLiveAgainstBaseline ./infrastructure/eval/
package eval_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	domainProject "github.com/gitagenthq/git-agent/domain/project"
	"github.com/gitagenthq/git-agent/infrastructure/eval"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
	"github.com/gitagenthq/git-agent/infrastructure/typesafe"
)

// readScopeList reads the repository's configured scopes as a list, falling back
// to a single unnamed scope so a repository without a project config can still
// be measured.
func readScopeList(t *testing.T, repo string) []domainProject.Scope {
	t.Helper()
	names := readScopes(t, repo)
	var scopes []domainProject.Scope
	for name, description := range names {
		scopes = append(scopes, domainProject.Scope{Name: name, Description: description})
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Name < scopes[j].Name })
	if len(scopes) == 0 {
		scopes = []domainProject.Scope{{Name: "", Description: "No scope is configured for this repository."}}
	}
	return scopes
}

// abConfig holds everything both sides need, so neither side can be measured
// under different inputs.
type abConfig struct {
	repo   string
	scopes []domainProject.Scope
}

// arm holds the two answers for one sample.
type arm struct {
	SHA                         string
	baselineType, baselineScope string
	jevType, jevScope           string
	baselineGroups              [][]string
	jevGroups                   [][]string
	wantType, wantScope         string
	ok                          bool
	// baselineOK records that the generative side answered. An A/B with a
	// missing arm is not an A/B: scoring an unavailable answer as a miss would
	// manufacture a difference between a model and a network failure.
	baselineOK bool
}

// TestLiveAgainstBaseline scores the generative path and the judgment on the
// same commits and prints the difference.
func TestLiveAgainstBaseline(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}
	repos := abRepos()
	limit := abLimit()

	for _, repo := range repos {
		t.Run(repoLabel(repo), func(t *testing.T) {
			measureRepo(t, repo, limit)
		})
	}
}

func measureRepo(t *testing.T, repo string, limit int) {
	t.Helper()
	cfg := abConfig{repo: repo, scopes: readScopeList(t, repo)}

	samples, err := eval.Collector{Dir: repo}.Collect(context.Background(), limit)
	if err != nil {
		t.Fatalf("collecting samples: %v", err)
	}
	// A sample with no conventional prefix cannot score, so it is reported and
	// dropped rather than counted as a hit.
	var usable []eval.Sample
	for _, s := range samples {
		if _, _, ok := eval.ConventionalPrefix(s.Title); ok {
			usable = append(usable, s)
		}
	}
	if len(usable) < 3 {
		t.Skipf("only %d samples carry a conventional prefix", len(usable))
	}

	projectCfg := &domainProject.Config{Scopes: cfg.scopes}
	generator := infraOpenAI.NewClient(
		os.Getenv("AB_API_KEY"), os.Getenv("AB_BASE_URL"), os.Getenv("AB_MODEL"),
		120*time.Second, 0, nil,
	)
	judge := typesafe.NewTypeScopeJudge(liveClient(t))
	grouper := typesafe.NewGroupDecider(liveClient(t))

	// Two passes, because the free shared gateway rate-limits and Jev does not.
	// Measuring both sides inside one loop starves the baseline and turns a
	// measurement into a comparison of nothing.
	delay := abDelay()
	var baselineErrors, judgmentErrors []string
	arms := make([]arm, 0, len(usable))
	for i, s := range usable {
		a := arm{SHA: s.SHA[:7]}
		a.wantType, a.wantScope, a.ok = eval.ConventionalPrefix(s.Title)
		change := stagedFor(s)

		if i > 0 && delay > 0 {
			time.Sleep(delay)
		}
		msg, genErr := generator.Generate(context.Background(), commit.GenerateRequest{
			Diff: change, Config: projectCfg, Language: "en",
		})
		if genErr != nil {
			baselineErrors = append(baselineErrors, fmt.Sprintf("%s generate: %v", a.SHA, genErr))
		} else {
			a.baselineType, a.baselineScope, _ = eval.ConventionalPrefix(msg.Title)
			a.baselineOK = true
		}

		plan, planErr := generator.Plan(context.Background(), commit.PlanRequest{
			StagedDiff: change, Config: projectCfg, MaxPlanFiles: 150,
		})
		if planErr != nil {
			baselineErrors = append(baselineErrors, fmt.Sprintf("%s plan: %v", a.SHA, planErr))
		} else {
			a.baselineGroups = groupsOf(plan)
		}

		verdict, jErr := judge.JudgeTypeScope(context.Background(), commit.TypeScopeRequest{
			Files: fileChanges(s), Scopes: scopeMap(cfg.scopes), Language: "en",
		})
		if jErr != nil {
			judgmentErrors = append(judgmentErrors, fmt.Sprintf("%s: %v", a.SHA, jErr))
		} else {
			a.jevType, a.jevScope = verdict.Type, verdict.Scope
		}

		buckets := bucketsFor(s)
		grouping, gErr := grouper.DecideGroups(context.Background(), commit.GroupDecisionRequest{
			Buckets: buckets, Scopes: scopeMap(cfg.scopes),
		})
		switch {
		case gErr != nil:
			judgmentErrors = append(judgmentErrors, fmt.Sprintf("%s group: %v", a.SHA, gErr))
		case len(buckets) > 1:
			groups, _ := grouping.DecidedGroups(commit.GroupDecisionRequest{Buckets: buckets})
			a.jevGroups = groups
		}

		t.Logf("%s want %s(%s) baseline %s(%s) jev %s(%s) groups base=%s jev=%s",
			a.SHA, a.wantType, orNone(a.wantScope),
			orNone(a.baselineType), orNone(a.baselineScope),
			orNone(a.jevType), orNone(a.jevScope),
			shapeOrNone(a.baselineGroups), shapeOrNone(a.jevGroups))
		arms = append(arms, a)
	}

	// An unavailable side aborts the measurement. Reporting a delta computed
	// against a missing arm would be worse than reporting nothing.
	if len(baselineErrors) > 0 {
		t.Fatalf("the generative baseline is unavailable for %d call(s), so no comparison is possible:\n  %s\n"+
			"Set AB_BASE_URL, AB_API_KEY and AB_MODEL to a provider that answers, or raise the rate limit.",
			len(baselineErrors), strings.Join(trim(baselineErrors, 5), "\n  "))
	}
	if len(judgmentErrors) > 0 {
		t.Fatalf("the judgment is unavailable for %d call(s):\n  %s",
			len(judgmentErrors), strings.Join(trim(judgmentErrors, 5), "\n  "))
	}

	printAB(t, cfg, arms)
}

func printAB(t *testing.T, cfg abConfig, arms []arm) {
	t.Helper()
	// Every arm is already known to have both answers: the caller aborts
	// otherwise, so nothing here can silently score a missing answer.
	scored := 0
	baseType, jevType := 0, 0
	baseScope, jevScope, scopeScored := 0, 0, 0
	groupAgree, groupCompared := 0, 0
	var typeEdges, scopeEdges, groupEdges []string

	for _, a := range arms {
		if !a.ok {
			continue
		}
		scored++
		if a.baselineType == a.wantType {
			baseType++
		}
		if a.jevType == a.wantType {
			jevType++
		} else {
			typeEdges = append(typeEdges, fmt.Sprintf("%s baseline %s->%s jev->%s",
				sha(a), a.wantType, orNone(a.baselineType), orNone(a.jevType)))
		}
		if a.wantScope != "" {
			scopeScored++
			if a.baselineScope == a.wantScope {
				baseScope++
			}
			if a.jevScope == a.wantScope {
				jevScope++
			} else {
				scopeEdges = append(scopeEdges, fmt.Sprintf("%s want %s baseline %s jev %s",
					sha(a), a.wantScope, orNone(a.baselineScope), orNone(a.jevScope)))
			}
		}
		if len(a.baselineGroups) > 0 && len(a.jevGroups) > 0 {
			groupCompared++
			if sameGrouping(a.baselineGroups, a.jevGroups) {
				groupAgree++
			} else {
				groupEdges = append(groupEdges, fmt.Sprintf("%s baseline %s jev %s",
					sha(a), shape(a.baselineGroups), shape(a.jevGroups)))
			}
		}
	}

	t.Logf("\n=== A/B on %s: %d scored samples ===", cfg.repo, scored)
	t.Logf("  type   : baseline %d/%d (%.0f%%)   jev %d/%d (%.0f%%)   delta %+d",
		baseType, scored, percent(baseType, scored), jevType, scored, percent(jevType, scored), jevType-baseType)
	t.Logf("  scope  : baseline %d/%d (%.0f%%)   jev %d/%d (%.0f%%)   delta %+d",
		baseScope, scopeScored, percent(baseScope, scopeScored),
		jevScope, scopeScored, percent(jevScope, scopeScored), jevScope-baseScope)
	t.Logf("  groups : identical %d/%d (%.0f%%)", groupAgree, groupCompared, percent(groupAgree, groupCompared))

	if len(typeEdges) > 0 {
		t.Logf("  type disagreements:\n    %s", strings.Join(trim(typeEdges, 12), "\n    "))
	}
	if len(scopeEdges) > 0 {
		t.Logf("  scope disagreements:\n    %s", strings.Join(trim(scopeEdges, 12), "\n    "))
	}
	if len(groupEdges) > 0 {
		t.Logf("  grouping disagreements:\n    %s", strings.Join(trim(groupEdges, 12), "\n    "))
	}
}

// stagedFor rebuilds the staged change the CLI would have planned for a commit,
// so both sides see the same input.
func stagedFor(s eval.Sample) *diff.StagedDiff {
	return &diff.StagedDiff{Files: s.Files, Content: s.Diff, Lines: strings.Count(s.Diff, "\n")}
}

// fileChanges renders a sample as the per-file summary the judgment reads.
func fileChanges(s eval.Sample) []commit.FileChange {
	counts := eval.ParseNumstat(s.Numstat)
	changes := make([]commit.FileChange, 0, len(s.Files))
	for _, file := range s.Files {
		change := commit.FileChange{Path: file}
		if n, found := counts[file]; found {
			change.Adds, change.Dels = n[0], n[1]
		}
		changes = append(changes, change)
	}
	return changes
}

// bucketsFor groups a sample's files by top-level directory, which is the
// candidate set the grouping judgment receives at run time.
func bucketsFor(s eval.Sample) []commit.FileBucket {
	counts := eval.ParseNumstat(s.Numstat)
	order := []string{}
	filesByDir := map[string][]string{}
	addsByDir := map[string]int{}
	for _, file := range s.Files {
		dir, _, found := strings.Cut(file, "/")
		if !found {
			dir = "(root)"
		}
		if _, seen := filesByDir[dir]; !seen {
			order = append(order, dir)
		}
		filesByDir[dir] = append(filesByDir[dir], file)
		if n, found := counts[file]; found {
			addsByDir[dir] += n[0]
		}
	}
	buckets := make([]commit.FileBucket, 0, len(order))
	for i, dir := range order {
		buckets = append(buckets, commit.FileBucket{
			ID: string(rune('a' + i)), Label: dir,
			Files: filesByDir[dir], Adds: addsByDir[dir],
		})
	}
	return buckets
}

func groupsOf(plan *commit.CommitPlan) [][]string {
	var out [][]string
	for _, g := range plan.Groups {
		out = append(out, g.Files)
	}
	return out
}

// sameGrouping reports whether two groupings cover the same files in the same
// groups, order included.
func sameGrouping(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func shapeOrNone(groups [][]string) string {
	if len(groups) == 0 {
		return "-"
	}
	return shape(groups)
}

func shape(groups [][]string) string {
	var parts []string
	for _, g := range groups {
		parts = append(parts, fmt.Sprintf("%d", len(g)))
	}
	return strings.Join(parts, "+") + " files"
}

func percent(hits, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(hits) / float64(of) * 100
}

func orNone(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func sha(a arm) string { return a.SHA }

func trim(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	out := append([]string(nil), values[:max]...)
	return append(out, fmt.Sprintf("... and %d more", len(values)-max))
}

func scopeMap(scopes []domainProject.Scope) map[string]string {
	out := make(map[string]string, len(scopes))
	for _, s := range scopes {
		description := s.Description
		if description == "" {
			description = "A configured scope with no description."
		}
		out[s.Name] = description
	}
	return out
}

// abRepos returns the repositories to measure, as absolute paths. A test runs
// with its own package directory as the working directory, so a relative path
// would read the wrong repository and silently measure no scopes at all.
func abRepos() []string {
	if one := os.Getenv("AB_REPO"); one != "" {
		return []string{absPath(one)}
	}
	return []string{repoRoot(".")}
}

// repoRoot resolves a directory to the root of its git repository.
func repoRoot(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return absPath(dir)
	}
	return strings.TrimSpace(string(out))
}

func absPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// abDelay spaces the baseline calls so the free shared gateway does not
// rate-limit the side being measured.
func abDelay() time.Duration {
	if v := os.Getenv("AB_DELAY"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", new(int)); err == nil && n == 1 {
			seconds := 0
			fmt.Sscanf(v, "%d", &seconds)
			if seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return 6 * time.Second
}

func abLimit() int {
	limit := 12
	if v := os.Getenv("AB_LIMIT"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &limit); n != 1 || err != nil {
			limit = 12
		}
	}
	return limit
}

func repoLabel(repo string) string {
	abs, err := os.Getwd()
	if err != nil {
		return repo
	}
	if repo == abs || strings.HasSuffix(abs, "/git-agent-cli") {
		return "this repository"
	}
	parts := strings.Split(strings.TrimRight(abs, "/"), "/")
	if n := len(parts); n > 0 {
		return parts[n-1]
	}
	return repo
}
