package git

import (
	"bytes"
	"context"
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
	contentOut, err := exec.CommandContext(ctx, "git", "diff", "--staged").Output()
	if err != nil {
		return nil, err
	}

	namesOut, err := exec.CommandContext(ctx, "git", "diff", "--staged", "--name-only").Output()
	if err != nil {
		return nil, err
	}

	content := string(contentOut)

	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(namesOut), "\n"), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}

	return &diff.StagedDiff{
		Files:   files,
		Content: content,
		Lines:   strings.Count(content, "\n"),
	}, nil
}

func (c *Client) Commit(ctx context.Context, message string, skipHooks bool) error {
	args := []string{"commit", "-m", message}
	if skipHooks {
		args = append(args, "--no-verify")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_AGENT=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Client) AddAll(ctx context.Context) error {
	return exec.CommandContext(ctx, "git", "add", "-A").Run()
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

func (c *Client) TopLevelDirs(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
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

func (c *Client) HooksPath(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "config", "core.hooksPath").Output()
	if err == nil {
		p := strings.TrimRight(string(out), "\n")
		if p != "" {
			return p, nil
		}
	}
	gitDir, err := c.GitDir(ctx)
	if err != nil {
		return "", err
	}
	return gitDir + "/hooks", nil
}

func (c *Client) UnstagedDiff(ctx context.Context) (*diff.StagedDiff, error) {
	contentOut, err := exec.CommandContext(ctx, "git", "diff").Output()
	if err != nil {
		return nil, err
	}

	namesOut, err := exec.CommandContext(ctx, "git", "diff", "--name-only").Output()
	if err != nil {
		return nil, err
	}

	content := string(contentOut)

	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(namesOut), "\n"), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}

	return &diff.StagedDiff{
		Files:   files,
		Content: content,
		Lines:   strings.Count(content, "\n"),
	}, nil
}

func (c *Client) StageFiles(ctx context.Context, files []string) error {
	args := append([]string{"add", "--"}, files...)
	return exec.CommandContext(ctx, "git", args...).Run()
}

func (c *Client) UnstageAll(ctx context.Context) error {
	return exec.CommandContext(ctx, "git", "reset", "HEAD").Run()
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
