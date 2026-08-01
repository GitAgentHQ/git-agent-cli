package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	pkgerrors "github.com/gitagenthq/git-agent/pkg/errors"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

// gitCmd builds a git command with core.quotePath disabled so non-ASCII file
// paths (e.g. CJK names) are emitted verbatim instead of octal-escaped. Escaped
// paths do not round-trip back into pathspecs, which breaks staging.
func gitCmd(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotePath=false"}, args...)...)
}

// StagedDiffNumStat returns the output of `git diff --staged --numstat`. The summary
// is small (one line per file) regardless of diff size, so
// it is safe to inline into LLM prompts when the raw diff is too large.
func (c *Client) StagedDiffNumStat(ctx context.Context) (string, error) {
	out, err := gitCmd(ctx, "diff", "--staged", "--numstat", "--ignore-submodules=all").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (c *Client) StagedDiff(ctx context.Context) (*diff.StagedDiff, error) {
	var contentOut, namesOut []byte
	var contentErr, namesErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		contentOut, contentErr = gitCmd(ctx, "diff", "--staged", "--ignore-submodules=all").Output()
	}()
	go func() {
		defer wg.Done()
		namesOut, namesErr = gitCmd(ctx, "diff", "--staged", "--name-status", "--ignore-submodules=all").Output()
	}()
	wg.Wait()

	if contentErr != nil {
		return nil, contentErr
	}
	if namesErr != nil {
		return nil, namesErr
	}

	content := string(contentOut)
	return &diff.StagedDiff{
		Files:   parseNameStatus(namesOut),
		Content: content,
		Lines:   strings.Count(content, "\n"),
	}, nil
}

func (c *Client) Commit(ctx context.Context, message string) (string, error) {
	return runCommit(ctx, "commit", "-m", message)
}

// runCommit executes a `git commit` variant and normalizes failures. Git reports
// an empty index ("nothing to commit") on stdout — not stderr — with exit 1, so
// combined output is captured: that case maps to ErrNothingToCommit (callers skip
// the group), and any other failure surfaces the real git output instead of a
// bare "exit status 1".
func runCommit(ctx context.Context, args ...string) (string, error) {
	cmd := gitCmd(ctx, args...)
	cmd.Env = append(os.Environ(), "GIT_AGENT=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte("nothing to commit")) ||
			bytes.Contains(out, []byte("nothing added to commit")) ||
			bytes.Contains(out, []byte("no changes added to commit")) {
			return "", pkgerrors.ErrNothingToCommit
		}
		return "", fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (c *Client) CommitHash(ctx context.Context) (string, error) {
	out, err := gitCmd(ctx, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *Client) AddAll(ctx context.Context) error {
	if out, err := gitCmd(ctx, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func (c *Client) CommitSubjects(ctx context.Context, max int) ([]string, error) {
	out, err := gitCmd(ctx, "log", "--format=%s", "--max-count", strconv.Itoa(max)).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
			return []string{}, nil
		}
		return nil, err
	}

	var subjects []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			subjects = append(subjects, line)
		}
	}
	return subjects, nil
}

// CommitLog returns one entry per commit formatted as "subject\n  file1\n  file2",
// giving the scope generator both the commit message and the files it touched.
func (c *Client) CommitLog(ctx context.Context, max int) ([]string, error) {
	// --format="<subject>" followed by --name-only emits:
	//   <subject>\n\nfile1\nfile2\n\n<subject>\n\n...
	// We use a sentinel to delimit commits reliably.
	out, err := gitCmd(ctx, "log",
		"--format=COMMIT_START%s", "--name-only", "--max-count", strconv.Itoa(max),
	).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
			return []string{}, nil
		}
		return nil, err
	}

	var entries []string
	var current strings.Builder
	fileCount := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "COMMIT_START") {
			if current.Len() > 0 {
				entries = append(entries, strings.TrimRight(current.String(), "\n"))
				current.Reset()
			}
			current.WriteString(line[len("COMMIT_START"):])
			fileCount = 0
			continue
		}
		if line != "" {
			// Cap per-commit file lists: a single mega-commit (vendor import,
			// generated dump) can touch hundreds of thousands of paths, which
			// would balloon the scope-generation prompt past the LLM input
			// ceiling. Mirrors the capFiles bound used for tracked-file lists.
			if fileCount >= 300 {
				continue
			}
			current.WriteString("\n  ")
			current.WriteString(line)
			fileCount++
		}
	}
	if current.Len() > 0 {
		entries = append(entries, strings.TrimRight(current.String(), "\n"))
	}
	return entries, nil
}

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, "__pycache__": true,
	".next": true, "out": true, "coverage": true,
}

func (c *Client) TopLevelDirs(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !skipDirs[e.Name()] {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs, nil
}

func (c *Client) ProjectFiles(ctx context.Context) ([]string, error) {
	// Try git ls-files first (works for repos with commits).
	out, err := gitCmd(ctx, "ls-files").Output()
	if err == nil {
		files := splitNonEmpty(string(out))
		if len(files) > 0 {
			return capFiles(files), nil
		}
	}

	// Fallback: filesystem walk (for zero-commit repos).
	var files []string
	if walkErr := walkFiles(".", &files); walkErr != nil {
		return nil, walkErr
	}
	return capFiles(files), nil
}

func walkFiles(root string, files *[]string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return fs.SkipDir
			}
			return nil
		}
		*files = append(*files, path)
		return nil
	})
}

func splitNonEmpty(s string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func capFiles(files []string) []string {
	if len(files) > 300 {
		return files[:300]
	}
	return files
}

// AllChangedFiles returns all changed file names (staged, unstaged, and
// untracked-but-not-gitignored) without modifying the git index.
//
// The staged and unstaged diffs use --name-status (parsed via parseNameStatus)
// rather than --name-only so that both sides of a rename are captured: git
// collapses a staged rename to a single R line, and --name-only would report
// only the new path, dropping the old-path deletion entirely. That silently
// left the deletion out of the plan, so it was committed by itself on the next
// run — splitting a move across two commits.
func (c *Client) AllChangedFiles(ctx context.Context) ([]string, error) {
	type src struct {
		label string
		out   []byte
		err   error
		parse func([]byte) []string
	}
	staged := src{label: "git diff --staged", parse: parseNameStatus}
	unstaged := src{label: "git diff", parse: parseNameStatus}
	untracked := src{label: "git ls-files --others", parse: splitUnquoted}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		staged.out, staged.err = gitCmd(ctx, "diff", "--staged", "--name-status").Output()
	}()
	go func() {
		defer wg.Done()
		unstaged.out, unstaged.err = gitCmd(ctx, "diff", "--name-status").Output()
	}()
	go func() {
		defer wg.Done()
		// --full-name forces paths relative to the repo root. Without it,
		// ls-files reports paths relative to the cwd while git diff reports
		// them relative to the root, so a commit run from a subdirectory would
		// later fail to stage untracked files (StageFiles adds from the root).
		untracked.out, untracked.err = gitCmd(ctx, "ls-files", "--others", "--exclude-standard", "--full-name").Output()
	}()
	wg.Wait()

	// Hard-fail on any error: silently dropping a source could omit files the
	// user expects to be committed.
	for _, s := range []src{staged, unstaged, untracked} {
		if s.err != nil {
			return nil, fmt.Errorf("%s: %w", s.label, s.err)
		}
	}

	seen := make(map[string]bool)
	var files []string
	for _, s := range []src{staged, unstaged, untracked} {
		for _, f := range s.parse(s.out) {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	return files, nil
}

// splitUnquoted splits git output into non-empty lines and removes git's
// C-style path quoting from each. Used for `ls-files` output, which is a plain
// path per line (no status prefix, so parseNameStatus does not apply).
func splitUnquoted(out []byte) []string {
	lines := splitNonEmpty(string(out))
	for i, f := range lines {
		lines[i] = gitUnquote(f)
	}
	return lines
}

// DetectRenames returns the file moves among the current changes: for each,
// the content tracked at Old was removed and reappears at New. Both paths of a
// move must be committed together, so the caller keeps each pair in one group.
//
// Pairing is done two ways, covering every staging state:
//   - git's own rename detection (`git diff HEAD --raw -M`) reports staged or
//     tracked moves directly as R lines;
//   - a worktree move leaves the new file untracked, which is invisible to
//     git's rename detection, so its content blob sha is matched against the
//     deleted files' pre-image shas.
//
// A sha pairing is only trusted when exactly one deletion and one addition
// share it, so duplicate-content files are never mis-paired into a move.
func (c *Client) DetectRenames(ctx context.Context) ([]diff.Rename, error) {
	// --no-abbrev emits full 40-char object shas so they compare equal to
	// hash-object output when pairing worktree moves by content.
	out, err := gitCmd(ctx, "diff", "HEAD", "--raw", "--no-abbrev", "-M").Output()
	if err != nil {
		// Fails on an unborn branch (no HEAD, nothing tracked yet) and on any
		// other git error. Rename detection is a best-effort grouping aid, so
		// degrade to "no renames" rather than failing the whole commit.
		return nil, nil
	}

	var renames []diff.Rename
	delByHash := make(map[string][]string) // pre-image sha -> deleted paths
	addByHash := make(map[string][]string) // blob sha -> added (tracked) paths

	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		// Raw format: ":<srcmode> <dstmode> <srcsha> <dstsha> <status>\t<path>"
		// with a second tab-separated path for R/C. The meta chunk before the
		// first tab is space-separated.
		parts := strings.Split(line, "\t")
		meta := strings.Fields(parts[0])
		if len(meta) < 5 || len(parts) < 2 {
			continue
		}
		status, srcSHA, dstSHA := meta[4], meta[2], meta[3]
		switch {
		case strings.HasPrefix(status, "R") && len(parts) >= 3:
			renames = append(renames, diff.Rename{Old: gitUnquote(parts[1]), New: gitUnquote(parts[2])})
		case status == "D":
			delByHash[srcSHA] = append(delByHash[srcSHA], gitUnquote(parts[1]))
		case status == "A":
			addByHash[dstSHA] = append(addByHash[dstSHA], gitUnquote(parts[1]))
		}
	}

	// Only worktree moves (deleted tracked file -> still-untracked new file)
	// remain unpaired above. Hash untracked files only when there is at least
	// one deletion to match, so ordinary commits skip the extra work.
	if len(delByHash) > 0 {
		shas, paths, herr := c.hashUntracked(ctx)
		if herr == nil {
			for i, sha := range shas {
				addByHash[sha] = append(addByHash[sha], paths[i])
			}
		}
	}

	for sha, dels := range delByHash {
		if adds := addByHash[sha]; len(dels) == 1 && len(adds) == 1 {
			renames = append(renames, diff.Rename{Old: dels[0], New: adds[0]})
		}
	}
	return renames, nil
}

// hashUntracked lists untracked (non-ignored) files and returns their content
// blob shas alongside the matching paths, in the same order. Paths are
// repo-root relative, so hashing runs from the root to resolve them.
func (c *Client) hashUntracked(ctx context.Context) (shas, paths []string, err error) {
	listOut, err := gitCmd(ctx, "ls-files", "--others", "--exclude-standard", "--full-name").Output()
	if err != nil {
		return nil, nil, err
	}
	paths = splitNonEmpty(string(listOut))
	if len(paths) == 0 {
		return nil, nil, nil
	}

	root, rootErr := c.RepoRoot(ctx)
	h := gitCmd(ctx, "hash-object", "--stdin-paths")
	if rootErr == nil {
		h.Dir = root
	}
	h.Stdin = bytes.NewReader(listOut)
	hashOut, err := h.Output()
	if err != nil {
		return nil, nil, err
	}
	shas = splitNonEmpty(string(hashOut))
	if len(shas) != len(paths) {
		return nil, nil, fmt.Errorf("hash-object returned %d shas for %d paths", len(shas), len(paths))
	}
	for i := range paths {
		paths[i] = gitUnquote(paths[i])
	}
	return shas, paths, nil
}

func (c *Client) IsGitRepo(ctx context.Context) bool {
	return gitCmd(ctx, "rev-parse", "--git-dir").Run() == nil
}

func (c *Client) RepoRoot(ctx context.Context) (string, error) {
	out, err := gitCmd(ctx, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		// .Output() drops stderr into ExitError.Stderr; surface git's own message
		// ("fatal: not a git repository ...") instead of a bare "exit status 128".
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (c *Client) GitDir(ctx context.Context) (string, error) {
	out, err := gitCmd(ctx, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (c *Client) UnstagedDiff(ctx context.Context) (*diff.StagedDiff, error) {
	var contentOut, namesOut []byte
	var contentErr, namesErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		contentOut, contentErr = gitCmd(ctx, "diff", "--ignore-submodules=all").Output()
	}()
	go func() {
		defer wg.Done()
		namesOut, namesErr = gitCmd(ctx, "diff", "--name-status", "--ignore-submodules=all").Output()
	}()
	wg.Wait()

	if contentErr != nil {
		return nil, contentErr
	}
	if namesErr != nil {
		return nil, namesErr
	}

	content := string(contentOut)
	return &diff.StagedDiff{
		Files:   parseNameStatus(namesOut),
		Content: content,
		Lines:   strings.Count(content, "\n"),
	}, nil
}

func (c *Client) StageFiles(ctx context.Context, files []string) error {
	// Run `git add` from the repository root so that paths — which git always
	// reports relative to the repo root — resolve correctly even when the CLI
	// was invoked from a subdirectory. Without this, a path like
	// "skills/apple-events/SKILL.md" is interpreted relative to the cwd
	// (e.g. .../event/skills/apple-events) and fails to match.
	root, rootErr := c.RepoRoot(ctx)
	addCmd := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "git", args...)
		if rootErr == nil {
			cmd.Dir = root
		}
		return cmd
	}

	// Use -f to re-stage files that were already in the index but match .gitignore
	// (they were explicitly tracked before UnstageAll removed them).
	args := append([]string{"add", "-f", "--"}, files...)
	if _, err := addCmd(args...).CombinedOutput(); err == nil {
		return nil
	}
	// Batch failed; retry file-by-file to isolate bad entries.
	var staged int
	var lastErr error
	for _, f := range files {
		if out, err := addCmd("add", "-f", "--", f).CombinedOutput(); err != nil {
			lastErr = fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
		} else {
			staged++
		}
	}
	if staged == 0 {
		return fmt.Errorf("no files could be staged: %w", lastErr)
	}
	if staged < len(files) {
		return fmt.Errorf("partial staging: %d/%d files staged, last error: %w", staged, len(files), lastErr)
	}
	return nil
}

func (c *Client) UnstageAll(ctx context.Context) error {
	if _, err := gitCmd(ctx, "reset", "HEAD").CombinedOutput(); err == nil {
		return nil
	}
	// In a repo with no commits yet HEAD cannot be resolved; remove everything
	// from the index instead.
	if _, err := gitCmd(ctx, "rm", "--cached", "-r", ".").CombinedOutput(); err != nil {
		// Both failed — index is already empty, which is the goal.
		return nil
	}
	return nil
}

// IsTracked reports whether path is tracked in the git index (repo-relative).
// Used to detect generated files (e.g. .git-agent/graph.db) that were committed
// before an ignore rule existed and so defeat .gitignore.
func (c *Client) IsTracked(ctx context.Context, path string) (bool, error) {
	root, err := c.RepoRoot(ctx)
	if err != nil {
		return false, fmt.Errorf("repo root: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--error-unmatch", "--", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// ls-files --error-unmatch exits non-zero with "did not match any files"
		// when the path is not tracked — that is the intended "not tracked" signal,
		// not a real error. Any other failure (corrupt repo, git missing) is rare
		// and not distinguishable here without parsing stderr; treat as not tracked.
		return false, nil
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// UntrackFile removes path from the git index while leaving the working-tree
// file in place (git rm --cached). Used by init to stop tracking generated
// databases that .gitignore cannot ignore because they are already tracked.
func (c *Client) UntrackFile(ctx context.Context, path string) error {
	root, err := c.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rm", "--cached", "--", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func (c *Client) LastCommitDiff(ctx context.Context) (*diff.StagedDiff, error) {
	contentOut, err := gitCmd(ctx, "diff", "HEAD~1..HEAD", "--ignore-submodules=all").Output()
	if err != nil {
		return nil, err
	}

	namesOut, err := gitCmd(ctx, "diff", "HEAD~1..HEAD", "--name-status", "--ignore-submodules=all").Output()
	if err != nil {
		return nil, err
	}

	content := string(contentOut)
	return &diff.StagedDiff{
		Files:   parseNameStatus(namesOut),
		Content: content,
		Lines:   strings.Count(content, "\n"),
	}, nil
}

func (c *Client) AmendCommit(ctx context.Context, message string) (string, error) {
	return runCommit(ctx, "commit", "--amend", "-m", message)
}

// FormatTrailers pipes message into `git interpret-trailers` and returns the
// formatted message with trailers appended according to git's trailer rules.
func (c *Client) FormatTrailers(ctx context.Context, message string, trailers []commit.Trailer) (string, error) {
	args := []string{"interpret-trailers", "--if-exists=addIfDifferent"}
	for _, t := range trailers {
		args = append(args, "--trailer", t.Key+": "+t.Value)
	}
	cmd := gitCmd(ctx, args...)
	cmd.Stdin = bytes.NewBufferString(message)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// gitUnquote removes git's C-style quoting from a path. Git wraps paths in
// double-quotes and escapes special bytes when they contain non-ASCII or
// special characters. If s is not quoted, it is returned unchanged.
func gitUnquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	s = s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++ // consume backslash
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		default:
			// Octal: three digits \NNN
			if s[i] >= '0' && s[i] <= '7' && i+2 < len(s) {
				n, err := strconv.ParseUint(s[i:i+3], 8, 8)
				if err == nil {
					b.WriteByte(byte(n))
					i += 3
					continue
				}
			}
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
		i++
	}
	return b.String()
}

// parseNameStatus parses `git diff --name-status` output into a deduplicated
// list of file paths. Rename/copy lines (R/C status) emit both old and new
// paths so that the old path (deletion) is also staged.
func parseNameStatus(out []byte) []string {
	seen := make(map[string]bool)
	var files []string
	add := func(p string) {
		p = gitUnquote(p)
		if p != "" && !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		switch {
		case strings.HasPrefix(status, "R"), strings.HasPrefix(status, "C"):
			// R100\told\tnew or C100\told\tnew
			if len(parts) >= 3 {
				add(parts[1])
				add(parts[2])
			}
		default:
			add(parts[1])
		}
	}
	return files
}
