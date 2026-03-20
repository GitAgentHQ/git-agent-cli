package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/fradser/git-agent/domain/commit"
	"github.com/fradser/git-agent/domain/diff"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) StagedDiff(ctx context.Context) (*diff.StagedDiff, error) {
	contentOut, err := exec.CommandContext(ctx, "git", "diff", "--staged", "--ignore-submodules=all").Output()
	if err != nil {
		return nil, err
	}

	namesOut, err := exec.CommandContext(ctx, "git", "diff", "--staged", "--name-status", "--ignore-submodules=all").Output()
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

func (c *Client) Commit(ctx context.Context, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Env = append(os.Environ(), "GIT_AGENT=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Client) AddAll(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "git", "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func (c *Client) CommitSubjects(ctx context.Context, max int) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "log", "--format=%s", "--max-count", strconv.Itoa(max)).Output()
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
	out, err := exec.CommandContext(ctx, "git", "log",
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
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "COMMIT_START") {
			if current.Len() > 0 {
				entries = append(entries, strings.TrimRight(current.String(), "\n"))
				current.Reset()
			}
			current.WriteString(line[len("COMMIT_START"):])
			continue
		}
		if line != "" {
			current.WriteString("\n  ")
			current.WriteString(line)
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
	out, err := exec.CommandContext(ctx, "git", "ls-files").Output()
	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	// Cap to avoid overwhelming the LLM prompt.
	if len(files) > 300 {
		files = files[:300]
	}
	return files, nil
}

func (c *Client) IsGitRepo(ctx context.Context) bool {
	return exec.CommandContext(ctx, "git", "rev-parse", "--git-dir").Run() == nil
}

func (c *Client) RepoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (c *Client) GitDir(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (c *Client) UnstagedDiff(ctx context.Context) (*diff.StagedDiff, error) {
	contentOut, err := exec.CommandContext(ctx, "git", "diff", "--ignore-submodules=all").Output()
	if err != nil {
		return nil, err
	}

	namesOut, err := exec.CommandContext(ctx, "git", "diff", "--name-status", "--ignore-submodules=all").Output()
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

func (c *Client) StageFiles(ctx context.Context, files []string) error {
	// Use -f to re-stage files that were already in the index but match .gitignore
	// (they were explicitly tracked before UnstageAll removed them).
	args := append([]string{"add", "-f", "--"}, files...)
	if _, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err == nil {
		return nil
	}
	// Batch failed; retry file-by-file to isolate bad entries.
	var staged int
	var lastErr error
	for _, f := range files {
		if out, err := exec.CommandContext(ctx, "git", "add", "-f", "--", f).CombinedOutput(); err != nil {
			lastErr = fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
		} else {
			staged++
		}
	}
	if staged == 0 {
		return fmt.Errorf("no files could be staged: %w", lastErr)
	}
	return nil
}

func (c *Client) UnstageAll(ctx context.Context) error {
	if _, err := exec.CommandContext(ctx, "git", "reset", "HEAD").CombinedOutput(); err == nil {
		return nil
	}
	// In a repo with no commits yet HEAD cannot be resolved; remove everything
	// from the index instead.
	if _, err := exec.CommandContext(ctx, "git", "rm", "--cached", "-r", ".").CombinedOutput(); err != nil {
		// Both failed — index is already empty, which is the goal.
		return nil
	}
	return nil
}

func (c *Client) LastCommitDiff(ctx context.Context) (*diff.StagedDiff, error) {
	contentOut, err := exec.CommandContext(ctx, "git", "diff", "HEAD~1..HEAD", "--ignore-submodules=all").Output()
	if err != nil {
		return nil, err
	}

	namesOut, err := exec.CommandContext(ctx, "git", "diff", "HEAD~1..HEAD", "--name-status", "--ignore-submodules=all").Output()
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

func (c *Client) AmendCommit(ctx context.Context, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "--amend", "-m", message)
	cmd.Env = append(os.Environ(), "GIT_AGENT=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// FormatTrailers pipes message into `git interpret-trailers` and returns the
// formatted message with trailers appended according to git's trailer rules.
func (c *Client) FormatTrailers(ctx context.Context, message string, trailers []commit.Trailer) (string, error) {
	args := []string{"interpret-trailers"}
	for _, t := range trailers {
		args = append(args, "--trailer", t.Key+": "+t.Value)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
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
