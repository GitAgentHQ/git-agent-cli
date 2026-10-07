package eval_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/infrastructure/eval"
)

// newRepo builds a small repository with three commits and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	commands := [][]string{
		{"init", "-q"},
		{"config", "user.email", "eval@example.test"},
		{"config", "user.name", "Eval"},
	}
	for _, args := range commands {
		run(t, dir, args...)
	}

	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "feat: add the entry point")

	write(t, dir, "service.go", "package main\n\nfunc serve() {}\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "fix(app): correct the service call")

	write(t, dir, "notes.md", "notes\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "chore: write release notes without a scope")

	// A merge commit must be skipped: its title describes no single change.
	run(t, dir, "checkout", "-q", "-b", "side")
	write(t, dir, "side.go", "package main\n\nfunc side() {}\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "feat: add a side path")
	run(t, dir, "checkout", "-q", "-")
	run(t, dir, "merge", "-q", "--no-ff", "-m", "Merge branch 'side'", "side")
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestCollector_ReadsOneSamplePerNonMergeCommit(t *testing.T) {
	dir := newRepo(t)

	samples, err := eval.Collector{Dir: dir}.Collect(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(samples) != 4 {
		t.Fatalf("expected four samples with the merge skipped, got %d: %v", len(samples), titles(samples))
	}
	for _, s := range samples {
		if strings.HasPrefix(s.Title, "Merge ") {
			t.Errorf("expected merge commits to be skipped, got %q", s.Title)
		}
		if len(s.Files) == 0 {
			t.Errorf("expected a file list for %s", short(s.SHA))
		}
		if s.Numstat == "" {
			t.Errorf("expected numstat for %s", short(s.SHA))
		}
		if s.DiffSize == 0 {
			t.Errorf("expected a measured diff size for %s", short(s.SHA))
		}
		if s.Diff == "" {
			t.Errorf("expected the change body for %s", short(s.SHA))
		}
		if s.Date.IsZero() {
			t.Errorf("expected a commit date for %s", short(s.SHA))
		}
	}
	// git orders by topology, not by wall clock, so the assertion is on the set
	// of samples rather than on a position.
	got := strings.Join(titles(samples), "|")
	for _, want := range []string{
		"feat: add the entry point",
		"fix(app): correct the service call",
		"chore: write release notes without a scope",
		"feat: add a side path",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected a sample titled %q, got %v", want, titles(samples))
		}
	}
}

func TestCollector_HonorsTheLimit(t *testing.T) {
	dir := newRepo(t)

	samples, err := eval.Collector{Dir: dir}.Collect(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 2 {
		t.Errorf("expected two samples, got %d", len(samples))
	}
}

func TestConventionalPrefix(t *testing.T) {
	cases := []struct {
		title     string
		wantType  string
		wantScope string
		wantOK    bool
	}{
		{"feat: add a thing", "feat", "", true},
		{"fix(app): correct a call", "fix", "app", true},
		{"refactor(cli)!: drop a command", "refactor", "cli", true},
		{"chore(deps): bump", "chore", "deps", true},
		{"no prefix at all here", "", "", false},
		{"", "", "", false},
		{"feat(): empty scope", "", "", false},
	}
	for _, tc := range cases {
		gotType, gotScope, ok := eval.ConventionalPrefix(tc.title)
		if ok != tc.wantOK || gotType != tc.wantType || gotScope != tc.wantScope {
			t.Errorf("ConventionalPrefix(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.title, gotType, gotScope, ok, tc.wantType, tc.wantScope, tc.wantOK)
		}
	}
}

func TestReport_FoldsSamplesIntoOneBlock(t *testing.T) {
	var report eval.Report
	report.Seam = "type_scope"

	sample := func(sha, title string) eval.Sample { return eval.Sample{SHA: sha, Title: title, DiffSize: 2048} }
	report.Record(sample("aaaaaaa1", "fix(app): one"), "fix", "app", "fix", "app", 0.9, 0, 100)
	report.Record(sample("bbbbbbb2", "feat: two"), "feat", "", "feat", "app", 0.4, 0, 100)
	report.RecordParts(sample("ccccccc3", "docs(cli): three"), "docs", "cli", "chore", "app",
		0.8, 0.8, 0.8, 0, 100)
	// A second miss at a much lower confidence, which is what a floor separates.
	report.RecordParts(sample("ddddddd4", "fix(app): four"), "fix", "app", "chore", "cli",
		0.44, 0.44, 0.44, 0, 100)
	report.Finish()

	if report.Samples != 4 || report.Judged != 4 {
		t.Fatalf("expected three judged samples, got %+v", report)
	}
	if report.PrefixHits != 1 {
		t.Errorf("expected one exact prefix, got %d", report.PrefixHits)
	}
	if report.TypeHits != 2 || report.ScopeHits != 2 {
		t.Errorf("expected 2 type and 2 scope agreements, got %d and %d", report.TypeHits, report.ScopeHits)
	}
	if len(report.TypeMisses) != 2 || !strings.Contains(report.TypeMisses[0], "ccccccc") {
		t.Errorf("expected the type miss to name its commit, got %v", report.TypeMisses)
	}
	if len(report.ScopeMisses) != 2 || !strings.Contains(report.ScopeMisses[0], "ccccccc") {
		t.Errorf("expected the scope miss to name its commit, got %v", report.ScopeMisses)
	}
	// A miss must carry the confidence it happened at, because that is the
	// number a floor would have to clear.
	if !strings.Contains(report.ScopeMisses[0], "0.80") {
		t.Errorf("expected the miss to report its confidence, got %v", report.ScopeMisses[0])
	}
	if report.ScopeConfidence < 0.6 {
		t.Errorf("expected the per-part confidences to be averaged separately, got type %.2f scope %.2f",
			report.TypeConfidence, report.ScopeConfidence)
	}
	if report.DiffBytesTotal != 8192 {
		t.Errorf("expected the left-out diff bytes to be summed, got %d", report.DiffBytesTotal)
	}

	rendered := report.String()
	for _, want := range []string{"type_scope", "prefix accuracy", "type agreement", "mean confidence", "diff left out"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected the report to mention %q, got:\n%s", want, rendered)
		}
	}
}

func TestReport_ZeroJudgedSamplesIsZeroAccuracy(t *testing.T) {
	var report eval.Report
	report.Seam = "type_scope"
	report.Finish()

	if report.Accuracy() != 0 {
		t.Errorf("expected zero accuracy with no judged sample, got %v", report.Accuracy())
	}
	if !strings.Contains(report.String(), "type_scope") {
		t.Error("expected an empty report to still name its seam")
	}
}

func TestParseNumstat(t *testing.T) {
	got := eval.ParseNumstat("3\t1\tmain.go\n-\t-\tassets/logo.png\nbad row\n7\t0\tsvc.go\n")

	if len(got) != 2 {
		t.Fatalf("expected only the countable rows, got %v", got)
	}
	if got["main.go"] != [2]int{3, 1} || got["svc.go"] != [2]int{7, 0} {
		t.Errorf("expected the parsed counts, got %v", got)
	}
}

func titles(samples []eval.Sample) []string {
	out := make([]string, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.Title)
	}
	return out
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
