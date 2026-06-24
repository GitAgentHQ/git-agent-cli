package application

import "strings"

// diffStat holds per-file line change counts parsed from a unified diff.
type diffStat struct {
	Additions int
	Deletions int
}

// parseDiffStat counts added and removed lines per file from a unified diff.
// It keys on the path in each "diff --git a/<p> b/<p>" header (git always names
// the real path on both sides, even for adds and deletes), then tallies content
// lines, ignoring the +++/--- file markers and @@ hunk headers. A file that
// appears in both the staged and unstaged sections accumulates across both.
func parseDiffStat(diff string) map[string]diffStat {
	stats := map[string]diffStat{}
	if diff == "" {
		return stats
	}
	var cur string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			cur = diffGitPath(line)
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
			// File markers, not content — skip.
		case strings.HasPrefix(line, "+"):
			if cur != "" {
				s := stats[cur]
				s.Additions++
				stats[cur] = s
			}
		case strings.HasPrefix(line, "-"):
			if cur != "" {
				s := stats[cur]
				s.Deletions++
				stats[cur] = s
			}
		}
	}
	return stats
}

// diffGitPath extracts the post-image path from a "diff --git a/<p> b/<p>" line.
// It anchors on the " a/" pre-image marker first, then locates " b/" after it,
// so a pre-image path that itself contains " b/" doesn't trigger a mis-parse.
func diffGitPath(line string) string {
	a := strings.Index(line, " a/")
	if a < 0 {
		return ""
	}
	rest := line[a+3:]
	if i := strings.Index(rest, " b/"); i >= 0 {
		return rest[i+3:]
	}
	return ""
}
