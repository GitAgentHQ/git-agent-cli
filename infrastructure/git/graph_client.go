package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// Compile-time check that GraphClient satisfies GraphGitClient.
var _ graph.GraphGitClient = (*GraphClient)(nil)

// GraphClient implements graph.GraphGitClient by executing git commands in a
// specific repository directory.
type GraphClient struct {
	repoPath string
}

// NewGraphClient creates a GraphClient rooted at the given repository path.
func NewGraphClient(repoPath string) *GraphClient {
	return &GraphClient{repoPath: repoPath}
}

// gitConfigArgs prepends settings that make git's path output verbatim and
// machine-parseable. core.quotePath=false stops git from octal-escaping
// non-ASCII filenames in --name-only/--raw/--numstat output, which would
// otherwise round-trip into broken pathspecs and silently-corrupt stored
// paths (the sibling client.go gitCmd applies the same fix).
var gitConfigArgs = []string{"-c", "core.quotePath=false"}

// run executes a git command in the repository directory and returns stdout.
func (g *GraphClient) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, gitConfigArgs...), args...)...)
	cmd.Dir = g.repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out), nil
}

func (g *GraphClient) runDiffAllowExitOne(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, gitConfigArgs...), args...)...)
	cmd.Dir = g.repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return string(out), nil
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out), nil
}

// runExitCode executes a git command and returns its exit code without
// treating exit code 1 as an error (useful for commands like merge-base
// --is-ancestor which use exit 1 to mean "false"). Exit codes >= 2 (including
// git's standard error code 128) are propagated as errors: silently treating
// them as a boolean "false" would cause callers to misinterpret a genuine git
// failure (e.g. an invalid/corrupt commit hash) as a normal negative result.
func (g *GraphClient) runExitCode(ctx context.Context, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, gitConfigArgs...), args...)...)
	cmd.Dir = g.repoPath
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ee.ExitCode() <= 1 {
			return ee.ExitCode(), nil
		}
		return ee.ExitCode(), fmt.Errorf("git %s: exit %d", strings.Join(args, " "), ee.ExitCode())
	}
	return -1, err
}

const commitSep = "COMMIT_START"

// CommitLogDetailed returns structured commit data from git log. If sinceHash
// is non-empty, only commits after that hash are returned. If maxCommits is
// positive, at most that many commits are returned.
func (g *GraphClient) CommitLogDetailed(ctx context.Context, sinceHash string, maxCommits int) ([]graph.CommitInfo, error) {
	args := []string{
		"log",
		"--format=" + commitSep + "%nH:%H%nM:%s%nAN:%an%nAE:%ae%nAT:%at%nP:%P",
		"--raw",
		"--numstat",
		"-M",
	}
	if maxCommits > 0 {
		args = append(args, "-n", strconv.Itoa(maxCommits))
	}
	if sinceHash != "" {
		args = append(args, sinceHash+"..HEAD")
	}

	out, err := g.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	return parseCommitLog(out), nil
}

// parseCommitLog parses the structured output from git log into CommitInfo
// slices. The format uses COMMIT_START as a delimiter between commits, with
// metadata on prefixed lines, --raw lines (starting with ":") for status, and
// --numstat lines for additions/deletions.
func parseCommitLog(raw string) []graph.CommitInfo {
	blocks := strings.Split(raw, commitSep+"\n")
	var commits []graph.CommitInfo

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		var ci graph.CommitInfo
		numStats := make(map[string][2]int) // path -> [additions, deletions]

		lines := strings.Split(block, "\n")
		for _, line := range lines {
			switch {
			case strings.HasPrefix(line, "H:"):
				ci.Hash = line[2:]
			case strings.HasPrefix(line, "M:"):
				ci.Message = line[2:]
			case strings.HasPrefix(line, "AN:"):
				ci.AuthorName = line[3:]
			case strings.HasPrefix(line, "AE:"):
				ci.AuthorEmail = line[3:]
			case strings.HasPrefix(line, "AT:"):
				ts, _ := strconv.ParseInt(line[3:], 10, 64)
				ci.Timestamp = ts
			case strings.HasPrefix(line, "P:"):
				parents := line[2:]
				if parents != "" {
					ci.ParentHashes = strings.Split(parents, " ")
				}
			case strings.HasPrefix(line, ":"):
				// --raw line: ":oldmode newmode oldhash newhash status\tpath[\tnewpath]"
				fc, ok := parseRawLine(line)
				if ok {
					ci.Files = append(ci.Files, fc)
				}
			default:
				parseNumStatLine(line, numStats)
			}
		}

		// Merge numstat data into file changes.
		for i := range ci.Files {
			key := ci.Files[i].Path
			if stats, found := numStats[key]; found {
				ci.Files[i].Additions = stats[0]
				ci.Files[i].Deletions = stats[1]
			}
		}

		if ci.Hash != "" {
			commits = append(commits, ci)
		}
	}

	return commits
}

// parseNumStatLine attempts to parse a --numstat line ("adds\tdels\tpath").
// Binary files show as "-\t-\tpath" and get 0/0. Returns true if parsed.
func parseNumStatLine(line string, stats map[string][2]int) bool {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return false
	}
	adds, errA := strconv.Atoi(parts[0])
	dels, errD := strconv.Atoi(parts[1])
	if errA != nil || errD != nil {
		// Binary files: "-\t-\tpath" -- record as 0/0.
		if parts[0] == "-" && parts[1] == "-" && parts[2] != "" {
			stats[parts[2]] = [2]int{0, 0}
			return true
		}
		return false
	}
	path := parts[2]
	// Renames in numstat show as "old => new" or "{old => new}/suffix".
	// Extract the new path for matching.
	if idx := strings.Index(path, " => "); idx >= 0 {
		// Handle "{prefix/old => new}/suffix" or plain "old => new"
		if braceOpen := strings.LastIndex(path[:idx], "{"); braceOpen >= 0 {
			braceClose := strings.Index(path[idx:], "}")
			if braceClose >= 0 {
				prefix := path[:braceOpen]
				suffix := path[idx+braceClose+1:]
				newPart := path[idx+4 : idx+braceClose]
				path = prefix + newPart + suffix
			}
		} else {
			path = path[idx+4:]
		}
	}
	stats[path] = [2]int{adds, dels}
	return true
}

// parseRawLine parses a --raw format line into a FileChange.
// Format: ":oldmode newmode oldhash newhash status\tpath[\tnewpath]"
func parseRawLine(line string) (graph.FileChange, bool) {
	// Split on tab to separate the header from the path(s).
	tabIdx := strings.IndexByte(line, '\t')
	if tabIdx < 0 {
		return graph.FileChange{}, false
	}
	header := line[:tabIdx]
	pathPart := line[tabIdx+1:]

	// Header: ":oldmode newmode oldhash newhash status"
	// The status is the last space-separated field.
	fields := strings.Fields(header)
	if len(fields) < 5 {
		return graph.FileChange{}, false
	}
	status := fields[4]

	paths := strings.Split(pathPart, "\t")

	switch {
	case strings.HasPrefix(status, "R"):
		if len(paths) < 2 {
			return graph.FileChange{}, false
		}
		return graph.FileChange{
			Status:  "R",
			OldPath: paths[0],
			Path:    paths[1],
		}, true
	case strings.HasPrefix(status, "C"):
		if len(paths) < 2 {
			return graph.FileChange{}, false
		}
		return graph.FileChange{
			Status:  "C",
			OldPath: paths[0],
			Path:    paths[1],
		}, true
	case status == "A" || status == "M" || status == "D":
		return graph.FileChange{
			Status: status,
			Path:   paths[0],
		}, true
	default:
		return graph.FileChange{}, false
	}
}

// CurrentHead returns the current HEAD commit hash.
func (g *GraphClient) CurrentHead(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// MergeBaseIsAncestor returns true if ancestor is an ancestor of head.
// It uses `git merge-base --is-ancestor` which exits 0 for true, 1 for false.
func (g *GraphClient) MergeBaseIsAncestor(ctx context.Context, ancestor, head string) (bool, error) {
	code, err := g.runExitCode(ctx, "merge-base", "--is-ancestor", ancestor, head)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// HashObject returns the git object hash for a file in the working tree.
// If the file does not exist, it returns "deleted" as a sentinel value.
func (g *GraphClient) HashObject(ctx context.Context, filePath string) (string, error) {
	fullPath := filePath
	if !strings.HasPrefix(filePath, "/") {
		fullPath = g.repoPath + "/" + filePath
	}
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "deleted", nil
	}
	out, err := g.run(ctx, "hash-object", filePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DiffNameOnly returns a sorted, deduplicated list of files with changes
// (staged, unstaged, and untracked).
func (g *GraphClient) DiffNameOnly(ctx context.Context) ([]string, error) {
	seen := make(map[string]bool)
	var files []string

	// Unstaged changes.
	unstaged, err := g.run(ctx, "diff", "--name-only")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Split(strings.TrimSpace(unstaged), "\n") {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	// Staged changes.
	staged, err := g.run(ctx, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Split(strings.TrimSpace(staged), "\n") {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	// Untracked files (new files not yet staged).
	untracked, err := g.run(ctx, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Split(strings.TrimSpace(untracked), "\n") {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	sort.Strings(files)
	return files, nil
}

// DiffNameOnlySince returns repo-relative paths changed in commits after
// sinceHash up to HEAD (sinceHash..HEAD). An empty sinceHash returns nil.
func (g *GraphClient) DiffNameOnlySince(ctx context.Context, sinceHash string) ([]string, error) {
	if sinceHash == "" {
		return nil, nil
	}
	out, err := g.run(ctx, "diff", "--name-only", sinceHash+"..HEAD")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(out), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files, nil
}

// DiffForFiles returns the combined diff output (staged + unstaged) for the
// specified files.
// TrackedFiles lists the git-tracked files under a path (a directory expands to
// its contents; a file returns itself). Paths are repo-relative, NUL-separated
// internally to survive unusual filenames.
func (g *GraphClient) TrackedFiles(ctx context.Context, path string) ([]string, error) {
	out, err := g.run(ctx, "ls-files", "-z", "--", path)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func (g *GraphClient) DiffForFiles(ctx context.Context, files []string) (string, error) {
	if len(files) == 0 {
		return "", nil
	}

	args := append([]string{"diff", "--"}, files...)
	unstaged, err := g.run(ctx, args...)
	if err != nil {
		return "", err
	}

	args = append([]string{"diff", "--cached", "--"}, files...)
	staged, err := g.run(ctx, args...)
	if err != nil {
		return "", err
	}

	untrackedOut, err := g.run(ctx, append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, files...)...)
	if err != nil {
		return "", err
	}
	var untracked strings.Builder
	for _, f := range strings.Split(strings.TrimRight(untrackedOut, "\x00"), "\x00") {
		if f == "" {
			continue
		}
		out, err := g.runDiffAllowExitOne(ctx, "diff", "--no-index", "--", "/dev/null", f)
		if err != nil {
			return "", err
		}
		untracked.WriteString(out)
	}

	combined := unstaged + staged + untracked.String()
	return strings.TrimRight(combined, "\n"), nil
}
