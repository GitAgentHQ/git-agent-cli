package commit_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/domain/commit"
)

// Scenario: A file list within the budget passes through unchanged.
func TestSummarizeFileList_WithinBudgetPassesThrough(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}

	summary := commit.SummarizeFileList(files, 150)

	if len(summary.Labels) != len(files) {
		t.Fatalf("expected %d labels, got %d: %v", len(files), len(summary.Labels), summary.Labels)
	}
	for i, f := range files {
		if summary.Labels[i] != f {
			t.Errorf("label %d = %q, want %q (labels must equal files verbatim)", i, summary.Labels[i], f)
		}
	}
	if summary.Expand != nil {
		t.Errorf("expected no expansion map, got %v", summary.Expand)
	}
}

// Scenario: An oversized flat directory collapses to one summary label.
func TestSummarizeFileList_OversizedDirectoryCollapsesToOneLabel(t *testing.T) {
	files := make([]string, 2000)
	for i := range files {
		files[i] = fmt.Sprintf("vendor/lib/file_%04d.c", i)
	}

	summary := commit.SummarizeFileList(files, 150)

	if len(summary.Labels) != 1 {
		t.Fatalf("expected exactly 1 label, got %d: %v", len(summary.Labels), summary.Labels)
	}
	wantLabel := "vendor/lib/ (2000 files)"
	if summary.Labels[0] != wantLabel {
		t.Fatalf("label = %q, want %q", summary.Labels[0], wantLabel)
	}
	expanded := summary.Expand[wantLabel]
	if len(expanded) != 2000 {
		t.Fatalf("expected 2000 expanded files, got %d", len(expanded))
	}
	want := make(map[string]bool, len(files))
	for _, f := range files {
		want[f] = true
	}
	for _, f := range expanded {
		if !want[f] {
			t.Errorf("expanded file %q was not in the original list", f)
		}
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("%d original files missing from expansion: %v", len(want), want)
	}
}

// Scenario: Collapsing rolls up only as many levels as needed — a small,
// shallow group stays listed individually while a large group collapses.
func TestSummarizeFileList_CollapsesOnlyTheOversizedGroup(t *testing.T) {
	var files []string
	for i := 0; i < 3; i++ {
		files = append(files, fmt.Sprintf("src/app/file_%d.go", i))
	}
	for i := 0; i < 200; i++ {
		files = append(files, fmt.Sprintf("vendor/lib/file_%04d.c", i))
	}

	summary := commit.SummarizeFileList(files, 150)

	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("src/app/file_%d.go", i)
		found := false
		for _, l := range summary.Labels {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected individual label %q, not found in %v", want, summary.Labels)
		}
	}
	wantCollapsed := "vendor/lib/ (200 files)"
	found := false
	for _, l := range summary.Labels {
		if l == wantCollapsed {
			found = true
		}
	}
	if !found {
		t.Errorf("expected collapsed label %q, not found in %v", wantCollapsed, summary.Labels)
	}
	if len(summary.Labels) != 4 {
		t.Errorf("expected 4 labels (3 individual + 1 collapsed), got %d: %v", len(summary.Labels), summary.Labels)
	}
}

// Scenario: A fully flat list with no shared subdirectory still collapses —
// the repository root itself is a valid, if maximally coarse, merge target.
func TestSummarizeFileList_FlatRootFilesCollapseToRoot(t *testing.T) {
	files := make([]string, 500)
	for i := range files {
		files[i] = fmt.Sprintf("file_%04d.txt", i)
	}

	summary := commit.SummarizeFileList(files, 150)

	if len(summary.Labels) != 1 {
		t.Fatalf("expected collapsing to root to produce 1 label, got %d: %v", len(summary.Labels), summary.Labels)
	}
	wantLabel := "./ (500 files)"
	if summary.Labels[0] != wantLabel {
		t.Fatalf("label = %q, want %q", summary.Labels[0], wantLabel)
	}
	if len(summary.Expand[wantLabel]) != 500 {
		t.Fatalf("expected 500 expanded files, got %d", len(summary.Expand[wantLabel]))
	}
}

// Regression: a later merge round can legitimately land on the same
// directory an earlier round already turned into its own bucket (a
// directory's direct files collapse first, then its subdirectories collapse
// up into the same directory a few rounds later). Buckets are keyed by
// prefix specifically so this can never produce two same-labelled buckets —
// confirm every original file is still accounted for exactly once and no
// label appears twice.
func TestSummarizeFileList_LaterRoundMergeIntoExistingBucketDoesNotCollide(t *testing.T) {
	var files []string
	for _, n := range []string{"a", "b", "c", "d"} {
		files = append(files, fmt.Sprintf("vendor/lib/%s.c", n))
	}
	for _, n := range []string{"w", "v"} {
		files = append(files, fmt.Sprintf("vendor/lib/subdir/%s.c", n))
	}
	for _, n := range []string{"o1", "o2"} {
		files = append(files, fmt.Sprintf("vendor/lib/other/%s.c", n))
	}

	summary := commit.SummarizeFileList(files, 2)

	seen := make(map[string]bool, len(summary.Labels))
	for _, l := range summary.Labels {
		if seen[l] {
			t.Fatalf("duplicate label %q in %v — two buckets collapsed to the same text", l, summary.Labels)
		}
		seen[l] = true
	}

	got := make(map[string]bool, len(files))
	for _, l := range summary.Labels {
		for _, f := range summary.Expand[l] {
			if got[f] {
				t.Errorf("file %q expanded from more than one label", f)
			}
			got[f] = true
		}
	}
	for _, f := range files {
		if !got[f] {
			t.Errorf("file %q missing from every label's expansion", f)
		}
	}
	if len(got) != len(files) {
		t.Errorf("expected %d total expanded files, got %d", len(files), len(got))
	}
}

// Regression: once several scattered top-level entries merge into a bucket
// keyed by the repository root (""), parentDir("") == "" makes that bucket
// list itself as one of its own byParent[""] entries on every later round.
// A prior version of the algorithm double-counted that self-reference as a
// genuine reduction opportunity, so it kept "merging" the root bucket with
// itself forever — a true infinite loop (reproduced against a real ~1600
// file changeset from a vendored-dependency-removal commit, not just this
// synthetic shape). Guard the test itself with a timeout so a regression
// fails loudly instead of hanging the suite.
func TestSummarizeFileList_RootSelfMergeDoesNotHang(t *testing.T) {
	var files []string
	// Many single, unrelated top-level entries — these merge into the root
	// bucket in an early round.
	for i := 0; i < 60; i++ {
		files = append(files, fmt.Sprintf("root_file_%03d.txt", i))
	}
	// Several distinct, moderately deep directory trees that still need
	// several more merge rounds after the root bucket already exists.
	for _, dir := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		for i := 0; i < 30; i++ {
			files = append(files, fmt.Sprintf("vendor/%s/sub/file_%03d.c", dir, i))
		}
	}

	done := make(chan commit.FileListSummary, 1)
	go func() {
		done <- commit.SummarizeFileList(files, 20)
	}()

	select {
	case summary := <-done:
		got := 0
		for _, label := range summary.Labels {
			got += len(summary.Expand[label])
			if summary.Expand[label] == nil {
				got++ // uncollapsed label is itself one real file
			}
		}
		if got != len(files) {
			t.Errorf("expected %d total files accounted for, got %d", len(files), got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SummarizeFileList did not terminate within 5s — regression of the root self-merge infinite loop")
	}
}

// A negative or zero budget disables collapsing entirely, matching the
// MaxDiffLines/MaxDiffBytes "0 = no limit" convention used elsewhere.
func TestSummarizeFileList_NonPositiveBudgetDisablesCollapsing(t *testing.T) {
	files := make([]string, 500)
	for i := range files {
		files[i] = fmt.Sprintf("vendor/lib/file_%04d.c", i)
	}

	summary := commit.SummarizeFileList(files, 0)

	if len(summary.Labels) != 500 {
		t.Fatalf("expected no collapsing with maxFiles=0, got %d labels", len(summary.Labels))
	}
	if summary.Expand != nil {
		t.Errorf("expected no expansion map, got %v", summary.Expand)
	}
}
