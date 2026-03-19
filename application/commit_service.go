package application

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/fradser/ga-cli/domain/commit"
	"github.com/fradser/ga-cli/domain/diff"
	"github.com/fradser/ga-cli/domain/hook"
	"github.com/fradser/ga-cli/domain/project"
)

var ErrHookBlocked = errors.New("hook blocked commit")

// CommitResult holds the output of a successful Commit call.
type CommitResult struct {
	Outline string
	DryRun  bool
}

type CommitGitClient interface {
	StagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	Commit(ctx context.Context, message string) error
	AddAll(ctx context.Context) error
}

type CommitRequest struct {
	Intent    string
	CoAuthor  string
	HookPath  string
	DryRun    bool
	All       bool
	Config    *project.Config
	MaxLines  int
	Verbose   bool
	LogWriter io.Writer // verbose-only output
	OutWriter io.Writer // always-visible output (hook block context, retries)
}

type CommitService struct {
	gen      commit.CommitMessageGenerator
	git      CommitGitClient
	hookExec hook.HookExecutor
}

func NewCommitService(gen commit.CommitMessageGenerator, git CommitGitClient, hookExec hook.HookExecutor) *CommitService {
	return &CommitService{gen: gen, git: git, hookExec: hookExec}
}

func (s *CommitService) vlog(req CommitRequest, format string, args ...any) {
	if req.Verbose && req.LogWriter != nil {
		fmt.Fprintf(req.LogWriter, format+"\n", args...)
	}
}

func (s *CommitService) out(req CommitRequest, format string, args ...any) {
	if req.OutWriter != nil {
		fmt.Fprintf(req.OutWriter, format+"\n", args...)
	}
}

const maxHookRetries = 3

func (s *CommitService) Commit(ctx context.Context, req CommitRequest) (*CommitResult, error) {
	if req.All {
		if err := s.git.AddAll(ctx); err != nil {
			return nil, fmt.Errorf("git add --all: %w", err)
		}
	}

	staged, err := s.git.StagedDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("staged diff: %w", err)
	}
	if len(staged.Files) == 0 {
		return nil, fmt.Errorf("no staged changes")
	}

	s.vlog(req, "staged files: %v", staged.Files)
	s.vlog(req, "diff lines: %d", staged.Lines)

	var hookFeedback string

	for attempt := 1; attempt <= maxHookRetries; attempt++ {
		s.vlog(req, "calling LLM... (attempt %d/%d)", attempt, maxHookRetries)

		msg, err := s.gen.Generate(ctx, commit.GenerateRequest{
			Diff:         staged,
			Intent:       req.Intent,
			Config:       req.Config,
			HookFeedback: hookFeedback,
		})
		if err != nil {
			return nil, fmt.Errorf("generate commit message: %w", err)
		}
		s.vlog(req, "LLM response received")

		assembled := msg.Title
		if msg.Body != "" {
			assembled += "\n\n" + msg.Body
		}
		if req.CoAuthor != "" {
			assembled += "\n\nCo-Authored-By: " + req.CoAuthor
		}

		if req.HookPath != "" {
			hookResult, err := s.hookExec.Execute(ctx, req.HookPath, hook.HookInput{
				Diff:          staged.Content,
				CommitMessage: assembled,
				Intent:        req.Intent,
				StagedFiles:   staged.Files,
				Config:        *req.Config,
			})
			if err != nil {
				return nil, fmt.Errorf("hook execute: %w", err)
			}
			if hookResult.ExitCode != 0 {
				s.out(req, "hook blocked (attempt %d/%d)", attempt, maxHookRetries)
				s.out(req, "commit message was:\n%s", assembled)
				if hookResult.Stderr != "" {
					s.out(req, "reason: %s", hookResult.Stderr)
				}
				hookFeedback = hookResult.Stderr
				if attempt < maxHookRetries {
					s.out(req, "retrying with hook feedback...")
				}
				continue
			}
		}

		result := &CommitResult{Outline: msg.Outline, DryRun: req.DryRun}
		if req.DryRun {
			return result, nil
		}
		if err := s.git.Commit(ctx, assembled); err != nil {
			return nil, err
		}
		return result, nil
	}

	return nil, ErrHookBlocked
}
