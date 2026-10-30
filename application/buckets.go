package application

import (
	"strconv"
	"strings"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/project"
)

// buildBuckets turns changed files into candidate buckets for the grouping
// judgment.
//
// The split rule is the one the heuristic planner already uses: the first path
// component, so every file under one top-level directory starts in one bucket.
// The buckets carry a structured summary rather than diff text: the label, the
// paths, and the line counts. Those are the signals measured to be enough for a
// judgment, and they keep the diff body out of a second provider.
//
// Line counts come from numstat when the caller has it, because the planning
// step runs before any diff body is fetched: the file lists are all that exists
// at that point.
func buildBuckets(diffs []*diff.StagedDiff, numstat string, maxBuckets int) []commit.FileBucket {
	var allFiles []string
	for _, d := range diffs {
		if d == nil {
			continue
		}
		allFiles = append(allFiles, d.Files...)
	}
	if len(allFiles) == 0 {
		return nil
	}

	// Per-file evidence, collected before bucketing so a file keeps its own
	// counts no matter which bucket it lands in.
	evidenceByFile := parseNumstat(numstat)
	for _, d := range diffs {
		for file, ev := range diffEvidence(d) {
			// A fetched diff body is the better source when it exists.
			if _, counted := evidenceByFile[file]; counted && ev.adds+ev.dels == 0 {
				continue
			}
			evidenceByFile[file] = ev
		}
	}

	var order []string
	bucketsByDir := make(map[string]*commit.FileBucket)
	for _, file := range allFiles {
		dir := topLevelComponent(file)
		bucket, ok := bucketsByDir[dir]
		if !ok {
			order = append(order, dir)
			bucket = &commit.FileBucket{Label: dir}
			bucketsByDir[dir] = bucket
		}
		bucket.Files = append(bucket.Files, file)
		if ev := evidenceByFile[file]; ev != nil {
			bucket.Adds += ev.adds
			bucket.Dels += ev.dels
			bucket.Symbols = append(bucket.Symbols, ev.symbols...)
		}
	}

	if maxBuckets > 0 && len(order) > maxBuckets {
		order = capBucketLabels(order, maxBuckets)
	}

	buckets := make([]commit.FileBucket, 0, len(order))
	for i, dir := range order {
		bucket := bucketsByDir[dir]
		if bucket == nil {
			continue
		}
		bucket.ID = bucketID(i)
		bucket.Symbols = dedupeStrings(bucket.Symbols, maxSymbolPerBucket)
		buckets = append(buckets, *bucket)
	}
	return buckets
}

// parseNumstat reads git numstat output: one "adds\tdels\tpath" row per file.
// A binary file row has "-" in place of a count and contributes nothing.
func parseNumstat(numstat string) map[string]*fileEvidence {
	out := map[string]*fileEvidence{}
	for _, line := range strings.Split(numstat, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 3 {
			continue
		}
		adds, errA := strconv.Atoi(strings.TrimSpace(fields[0]))
		dels, errD := strconv.Atoi(strings.TrimSpace(fields[1]))
		if errA != nil || errD != nil {
			continue
		}
		out[fields[2]] = &fileEvidence{adds: adds, dels: dels}
	}
	return out
}

// maxSymbolPerBucket caps the symbol list of one bucket so a large directory
// cannot dominate the state.
const maxSymbolPerBucket = 12

func bucketID(i int) string {
	// Letters keep the ids short and readable in a recorded observation.
	const letters = "abcdefghijklmnopqrstuvwxyz"
	if i < len(letters) {
		return string(letters[i])
	}
	return "b" + itoa(i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// capBucketLabels keeps the first maxBuckets directories and folds the rest into
// the last kept one, so every file still lands in a bucket.
func capBucketLabels(order []string, maxBuckets int) []string {
	if len(order) <= maxBuckets {
		return order
	}
	kept := append([]string(nil), order[:maxBuckets-1]...)
	return append(kept, order[maxBuckets-1:]...)
}

// fileEvidence is what one file's diff contributes to a bucket: its line counts
// and the symbols its hunk headers name.
type fileEvidence struct {
	adds, dels int
	symbols    []string
}

// diffEvidence summarizes a diff per file.
func diffEvidence(d *diff.StagedDiff) map[string]*fileEvidence {
	out := make(map[string]*fileEvidence)
	current := ""
	for _, line := range strings.Split(d.Content, "\n") {
		if strings.HasPrefix(line, "+++ ") {
			current = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			if i := strings.Index(current, "\t"); i >= 0 {
				current = current[:i]
			}
			if _, ok := out[current]; !ok {
				out[current] = &fileEvidence{}
			}
			continue
		}
		if current == "" {
			continue
		}
		ev := out[current]
		switch {
		case strings.HasPrefix(line, "+"):
			ev.adds++
		case strings.HasPrefix(line, "---"):
			// The file header of the old path, not a removed line.
		case strings.HasPrefix(line, "-"):
			ev.dels++
		case strings.HasPrefix(line, "@@"):
			if symbol := hunkSymbol(line); symbol != "" {
				ev.symbols = append(ev.symbols, symbol)
			}
		}
	}
	return out
}

// hunkSymbol extracts the function or type name a hunk header ends with, which
// is the strongest cheap signal for what a file's change is about.
func hunkSymbol(line string) string {
	start := strings.Index(line, "@@")
	if start < 0 {
		return ""
	}
	rest := line[start+2:]
	end := strings.Index(rest, "@@")
	if end < 0 {
		return ""
	}
	symbol := strings.TrimSpace(rest[end+2:])
	if len(symbol) > 80 {
		symbol = symbol[:80]
	}
	return symbol
}

func dedupeStrings(values []string, max int) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) == max {
			break
		}
	}
	return out
}

// scopeMap turns configured scopes into the name-to-description map the
// judgments carry.
func scopeMap(scopes []project.Scope) map[string]string {
	out := make(map[string]string, len(scopes))
	for _, s := range scopes {
		if s.Name == "" {
			continue
		}
		description := s.Description
		if description == "" {
			description = "A configured scope with no description."
		}
		out[s.Name] = description
	}
	return out
}

// groupingSignature renders a grouping as a canonical string, so a shadowed
// judgment can be compared with the grouping the planner produced.
func groupingSignature(groups [][]string) string {
	var parts []string
	for _, files := range groups {
		sorted := append([]string(nil), files...)
		sortStrings(sorted)
		parts = append(parts, strings.Join(sorted, "+"))
	}
	return strings.Join(parts, "|")
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
