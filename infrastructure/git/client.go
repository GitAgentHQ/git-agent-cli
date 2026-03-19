package git

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/fradser/ga-cli/domain/diff"
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

func (c *Client) Commit(ctx context.Context, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
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
