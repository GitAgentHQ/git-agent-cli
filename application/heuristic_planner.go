package application

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/project"
)

// NewDirectoryBucketer returns the deterministic fallback planner used when
// the LLM planner returns commit.ErrPlannerBudgetExhausted and the project
// opts in via plan_fallback=heuristic. The bucketer groups files by their
// top-level directory and labels each group with the matching configured
// scope (matched case-insensitively against the scope description).
func NewDirectoryBucketer() commit.HeuristicPlanner {
	return &directoryBucketer{}
}

type directoryBucketer struct{}

// Plan buckets req.StagedDiff.Files and req.UnstagedDiff.Files by their first
// path component, then caps the result at maxCommitGroups (5) by merging the
// smallest buckets into the last group. Returns a CommitPlan with placeholder
// titles of the form "chore(<scope>): update N files in <dir>/" — the
// downstream Generate loop replaces the title with a real LLM message before
// committing.
func (b *directoryBucketer) Plan(_ context.Context, req commit.PlanRequest) (*commit.CommitPlan, error) {
	var files []string
	if req.StagedDiff != nil {
		files = append(files, req.StagedDiff.Files...)
	}
	if req.UnstagedDiff != nil {
		files = append(files, req.UnstagedDiff.Files...)
	}
	if len(files) == 0 {
		return &commit.CommitPlan{}, nil
	}

	// Bucket by first path component while preserving first-seen order so
	// the resulting plan is stable across runs.
	type bucket struct {
		dir   string
		files []string
	}
	indexByDir := make(map[string]int)
	var buckets []bucket
	for _, f := range files {
		dir := topLevelComponent(f)
		idx, ok := indexByDir[dir]
		if !ok {
			indexByDir[dir] = len(buckets)
			buckets = append(buckets, bucket{dir: dir, files: []string{f}})
			continue
		}
		buckets[idx].files = append(buckets[idx].files, f)
	}

	// Cap at maxCommitGroups by merging the smallest buckets into the
	// already-last bucket so its file list stays cohesive when sorted by
	// the original top-level dir.
	if len(buckets) > maxCommitGroups {
		// Sort surplus buckets by size ascending; smallest gets merged first.
		sort.SliceStable(buckets[maxCommitGroups-1:], func(i, j int) bool {
			a := buckets[maxCommitGroups-1+i]
			c := buckets[maxCommitGroups-1+j]
			return len(a.files) < len(c.files)
		})
		mergeTarget := &buckets[maxCommitGroups-1]
		surplus := buckets[maxCommitGroups:]
		for _, s := range surplus {
			mergeTarget.files = append(mergeTarget.files, s.files...)
		}
		buckets = buckets[:maxCommitGroups]
	}

	var scopes []project.Scope
	if req.Config != nil {
		scopes = req.Config.Scopes
	}

	groups := make([]commit.CommitGroup, 0, len(buckets))
	for _, b := range buckets {
		scope := matchScope(b.dir, scopes)
		title := formatBucketTitle(scope, len(b.files), b.dir)
		groups = append(groups, commit.CommitGroup{
			Files:   b.files,
			Message: commit.CommitMessage{Title: title},
		})
	}
	return &commit.CommitPlan{Groups: groups}, nil
}

// topLevelComponent returns the first segment of a forward-slash path; a path
// without a separator is returned verbatim so root-level files form their own
// bucket.
func topLevelComponent(p string) string {
	cleaned := path.Clean(p)
	if i := strings.Index(cleaned, "/"); i >= 0 {
		return cleaned[:i]
	}
	return cleaned
}

// matchScope picks the scope whose description mentions dir as a
// case-insensitive substring. When no scope matches, the empty string falls
// through and the caller emits an unscoped placeholder title.
func matchScope(dir string, scopes []project.Scope) string {
	if len(scopes) == 0 || dir == "" {
		return ""
	}
	dirLower := strings.ToLower(dir)
	for _, s := range scopes {
		if strings.Contains(strings.ToLower(s.Description), dirLower) {
			return s.Name
		}
	}
	return ""
}

// formatBucketTitle renders the placeholder commit title; the Generate loop
// replaces it with an LLM-authored message before each commit lands.
func formatBucketTitle(scope string, fileCount int, dir string) string {
	if scope == "" {
		return fmt.Sprintf("chore: update %d files in %s/", fileCount, dir)
	}
	return fmt.Sprintf("chore(%s): update %d files in %s/", scope, fileCount, dir)
}
