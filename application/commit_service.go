package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/fradser/ga-cli/domain/commit"
	"github.com/fradser/ga-cli/domain/diff"
	"github.com/fradser/ga-cli/domain/hook"
	"github.com/fradser/ga-cli/domain/project"
)

var ErrHookBlocked = errors.New("hook blocked commit")

type CommitGitClient interface {
	StagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	Commit(ctx context.Context, message string) error
	AddAll(ctx context.Context) error
}

type CommitRequest struct {
	Intent   string
	CoAuthor string
	HookPath string
	DryRun   bool
	All      bool
	Config   *project.Config
	MaxLines int
}

type CommitService struct {
	gen      commit.CommitMessageGenerator
	git      CommitGitClient
	hookExec hook.HookExecutor
}

func NewCommitService(gen commit.CommitMessageGenerator, git CommitGitClient, hookExec hook.HookExecutor) *CommitService {
	return &CommitService{gen: gen, git: git, hookExec: hookExec}
}

func (s *CommitService) Commit(ctx context.Context, req CommitRequest) error {
	if req.All {
		if err := s.git.AddAll(ctx); err != nil {
			return fmt.Errorf("git add --all: %w", err)
		}
	}

	staged, err := s.git.StagedDiff(ctx)
	if err != nil {
		return fmt.Errorf("staged diff: %w", err)
	}
	if len(staged.Files) == 0 {
		return fmt.Errorf("no staged changes")
	}

	msg, err := s.gen.Generate(ctx, commit.GenerateRequest{
		Diff:   staged,
		Intent: req.Intent,
		Config: req.Config,
	})
	if err != nil {
		return fmt.Errorf("generate commit message: %w", err)
	}

	assembled := msg.Title
	if msg.Body != "" {
		assembled += "\n\n" + msg.Body
	}
	if req.CoAuthor != "" {
		assembled += "\n\nCo-Authored-By: " + req.CoAuthor
	}

	if req.HookPath != "" {
		result, err := s.hookExec.Execute(ctx, req.HookPath, hook.HookInput{
			Diff:          staged.Content,
			CommitMessage: assembled,
			Intent:        req.Intent,
			StagedFiles:   staged.Files,
			Config:        *req.Config,
		})
		if err != nil {
			return fmt.Errorf("hook execute: %w", err)
		}
		if result.ExitCode != 0 {
			return ErrHookBlocked
		}
	}

	if req.DryRun {
		return nil
	}

	return s.git.Commit(ctx, assembled)
}
