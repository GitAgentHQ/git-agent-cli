package application_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/project"
)

// regionReportDiff builds one git-style patch per file, with two anchored
// regions in each file. It is the smallest diff that triggers the user-driven
// split report.
func regionReportDiff(files ...string) *diff.StagedDiff {
	var content strings.Builder
	for _, file := range files {
		fmt.Fprintf(&content, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n"+
			"@@ -1 +1 @@ func First\n-old\n+new\n"+
			"@@ -10 +10 @@ func Second\n-old\n+new\n", file, file, file, file)
	}
	return &diff.StagedDiff{
		Files:   files,
		Content: content.String(),
		Lines:   strings.Count(content.String(), "\n"),
	}
}

// regionReportRun drives the public Commit seam with a remainder diff whose
// files all hold two regions. It returns the always-on warning stream and the
// verbose stream separately.
func regionReportRun(t *testing.T, files []string, verbose bool) (string, string) {
	t.Helper()
	groupDiff := &diff.StagedDiff{Files: files, Content: "+one change", Lines: 1}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			{}, // commitStagedContent: the index is empty
			groupDiff,
		},
		stagedDiff:      groupDiff,
		unstagedDiff:    regionReportDiff(files...),
		allChangedFiles: files,
	}
	svc := application.NewCommitService(
		&mockCommitGenerator{msg: defaultMsg()},
		&mockCommitPlanner{plan: singleGroupPlan(files)},
		git,
		noopHook(),
		nil, nil, nil, nil,
	)

	var warn, log bytes.Buffer
	_, err := svc.Commit(context.Background(), application.CommitRequest{
		Config:     &project.Config{},
		Verbose:    verbose,
		WarnWriter: &warn,
		LogWriter:  &log,
	})
	if err != nil {
		t.Fatalf("unexpected Commit error: %v", err)
	}
	return warn.String(), log.String()
}

func regionReportLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestCommitService_FewMultiRegionFiles_KeepPerFileWarnings(t *testing.T) {
	warn, _ := regionReportRun(t, []string{"a.go", "b.go"}, false)

	lines := regionReportLines(warn)
	if len(lines) != 2 {
		t.Fatalf("want one warning per file for a small run, got %d:\n%s", len(lines), warn)
	}
	for _, file := range []string{"a.go", "b.go"} {
		if !strings.Contains(warn, "git add -p "+file) {
			t.Fatalf("missing per-file report for %s:\n%s", file, warn)
		}
	}
}

func TestCommitService_ThreeMultiRegionFiles_KeepPerFileWarnings(t *testing.T) {
	warn, _ := regionReportRun(t, []string{"a.go", "b.go", "c.go"}, false)

	lines := regionReportLines(warn)
	if len(lines) != 3 {
		t.Fatalf("want one warning per file at the three-file boundary, got %d:\n%s", len(lines), warn)
	}
	for _, file := range []string{"a.go", "b.go", "c.go"} {
		if !strings.Contains(warn, "git add -p "+file) {
			t.Fatalf("missing per-file report for %s:\n%s", file, warn)
		}
	}
	if strings.Contains(warn, "--verbose") {
		t.Fatalf("a small run must not print a bounded-summary hint:\n%s", warn)
	}
}

func TestCommitService_ManyMultiRegionFiles_OneSummaryOnWarnWriter(t *testing.T) {
	warn, log := regionReportRun(t, []string{"a.go", "b.go", "c.go", "d.go"}, false)

	lines := regionReportLines(warn)
	if len(lines) != 1 {
		t.Fatalf("want one bounded warning for four multi-region files, got %d:\n%s", len(lines), warn)
	}
	if !strings.Contains(lines[0], "4 files hold separate regions") {
		t.Fatalf("summary must state the number of affected files, got:\n%s", lines[0])
	}
	if !strings.Contains(lines[0], "git add -p a.go") {
		t.Fatalf("summary must name a concrete command, got:\n%s", lines[0])
	}
	if got := strings.Count(warn, "git add -p "); got != 1 {
		t.Fatalf("want exactly one git add -p command, got %d:\n%s", got, warn)
	}
	if !strings.Contains(lines[0], "--verbose") {
		t.Fatalf("summary must point at --verbose for full details, got:\n%s", lines[0])
	}
	if log != "" {
		t.Fatalf("a non-verbose run must not write the full detail list, got:\n%s", log)
	}
}

func TestCommitService_ManyMultiRegionFiles_VerboseListsEveryFile(t *testing.T) {
	warn, log := regionReportRun(t, []string{"a.go", "b.go", "c.go", "d.go"}, true)

	if got := len(regionReportLines(warn)); got != 1 {
		t.Fatalf("verbose must keep the warning stream bounded, got %d:\n%s", got, warn)
	}
	if strings.Contains(warn, "--verbose") {
		t.Fatalf("verbose summary must not repeat a hint for a flag already set:\n%s", warn)
	}
	for _, file := range []string{"a.go", "b.go", "c.go", "d.go"} {
		if !strings.Contains(log, "git add -p "+file) {
			t.Fatalf("verbose details missing %s:\n%s", file, log)
		}
	}
	if got := strings.Count(log, "holds 2 separate regions"); got != 4 {
		t.Fatalf("want four per-file detail lines, got %d:\n%s", got, log)
	}
}
