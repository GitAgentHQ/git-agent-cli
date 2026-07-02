package commit

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultMaxPlanFiles caps the number of individual file paths listed in the
// planner prompt before SummarizeFileList collapses them into directory-level
// summaries. A commit touching, say, a vendored dependency directory can
// stage thousands of files; sending every path as its own prompt line burns
// tokens the planner does not need (a directory summary conveys the same
// grouping signal) and can stall a small/cheap model outright. Override with
// --max-plan-files / max_plan_files for endpoints that can handle more.
const DefaultMaxPlanFiles = 150

// FileListSummary is a token-bounded rendering of a file list for an LLM
// prompt. When the input already fits within the requested budget, Labels
// holds the files verbatim and Expand is nil. Otherwise Labels holds
// directory-level summaries (e.g. "vendor/lib/ (842 files)") produced by
// collapsing only the directories that need it, and Expand maps each
// summary label back to the real file paths it stands in for, so a caller
// can recover the exact files an LLM referenced a label for.
type FileListSummary struct {
	Labels []string
	Expand map[string][]string
}

// SummarizeFileList collapses files into directory-level groups when the
// list is too large to send to an LLM planner one path per line. maxFiles
// <= 0 disables collapsing.
//
// Buckets are stored keyed by their current directory prefix (never as a
// plain list), so two buckets can never independently converge on the same
// prefix — folding always lands in the single existing entry for that
// prefix instead of creating a duplicate. This matters because collapsing
// happens over several rounds and a later round's merge can legitimately
// land on a directory an earlier round already turned into a bucket (e.g.
// "vendor/lib/" holding its own direct files, then later also absorbing
// "vendor/lib/sub/" and "vendor/lib/other/" once those merge up a level);
// without the map keying, that would silently produce two same-labelled
// buckets and one would overwrite the other in the returned Expand map.
//
// On each round, the algorithm looks at every bucket's immediate parent
// directory and merges into whichever parent currently absorbs the most
// files — repeating until the bucket count fits the budget or no merge
// would reduce the count any further. This "biggest offender first" order
// means a directory with only a handful of files is left listed
// individually unless collapsing it turns out to be unavoidable, while a
// directory with hundreds of files collapses immediately. In the worst case
// every bucket eventually shares the repository root as a common parent, so
// collapsing always terminates with at least one valid (if maximally
// coarse) summary.
func SummarizeFileList(files []string, maxFiles int) FileListSummary {
	if maxFiles <= 0 || len(files) <= maxFiles {
		return FileListSummary{Labels: append([]string(nil), files...)}
	}

	buckets := make(map[string][]string, len(files)) // prefix -> member files
	for _, f := range files {
		buckets[f] = append(buckets[f], f)
	}

	for len(buckets) > maxFiles {
		// Group current bucket prefixes by the parent directory a merge
		// would land them in.
		byParent := make(map[string][]string, len(buckets))
		for prefix := range buckets {
			parent := parentDir(prefix)
			byParent[parent] = append(byParent[parent], prefix)
		}

		var bestParent string
		bestSize := 0 // total files under bestParent; tie-break toward the larger group
		found := false
		for parent, srcs := range byParent {
			// The repository root is the one fixed point of parentDir ("" maps
			// to itself), so once a bucket is keyed exactly "" it lists itself
			// as one of its own byParent[""] entries. Count it only once —
			// either as the "parent already exists as a bucket" bonus below,
			// or as a member of srcs, never both, or a self-merge would look
			// like it reduces the bucket count when it actually changes
			// nothing (infinite loop).
			selfIncluded := false
			for _, p := range srcs {
				if p == parent {
					selfIncluded = true
					break
				}
			}
			_, parentIsBucket := buckets[parent]
			before := len(srcs)
			if parentIsBucket && !selfIncluded {
				before++
			}
			if before-1 < 1 {
				continue // merging here would not reduce the bucket count
			}
			size := 0
			for _, p := range srcs {
				size += len(buckets[p])
			}
			if parentIsBucket && !selfIncluded {
				size += len(buckets[parent])
			}
			if !found || size > bestSize || (size == bestSize && parent < bestParent) {
				found, bestParent, bestSize = true, parent, size
			}
		}
		if !found {
			break // no merge would reduce the bucket count any further
		}

		merged := append([]string(nil), buckets[bestParent]...) // nil-safe if bestParent has no bucket yet
		for _, p := range byParent[bestParent] {
			if p == bestParent {
				continue // already folded in above
			}
			merged = append(merged, buckets[p]...)
			delete(buckets, p)
		}
		buckets[bestParent] = merged
	}

	labels := make([]string, 0, len(buckets))
	expand := make(map[string][]string, len(buckets))
	for prefix, members := range buckets {
		if len(members) == 1 && members[0] == prefix {
			labels = append(labels, prefix) // never collapsed — real path
			continue
		}
		label := fmt.Sprintf("%s/ (%d files)", displayDir(prefix), len(members))
		labels = append(labels, label)
		expand[label] = members
	}
	sort.Strings(labels)
	return FileListSummary{Labels: labels, Expand: expand}
}

// parentDir returns p with its last "/"-separated segment removed, or "" if
// p has no separator (i.e. it is already at the repository root).
func parentDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i]
}

// displayDir renders a possibly-empty directory prefix for a summary label;
// "" (repository root) reads as "." rather than an empty string.
func displayDir(p string) string {
	if p == "" {
		return "."
	}
	return p
}
