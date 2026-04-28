package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/project"
)

func TestCommitService_NoStagedChanges(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiff:      &diff.StagedDiff{}, // nothing pre-staged
		allChangedFiles: []string{},         // no changed files at all
	}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)

	if err == nil {
		t.Fatal("expected error for empty changes, got nil")
	}
	if !strings.Contains(err.Error(), "no changes") {
		t.Errorf("expected error containing 'no changes', got: %v", err)
	}
}

func TestCommitService_PlannerReturnsEmptyPlan(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
		},
		allChangedFiles: []string{"main.go", "b.go"},
	}
	planner := &mockCommitPlanner{
		plan: &commit.CommitPlan{Groups: []commit.CommitGroup{
			{Files: []string{"hallucinated.go"}},
		}},
	}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)

	if err == nil {
		t.Fatal("expected error for plan with only hallucinated files, got nil")
	}
	if !strings.Contains(err.Error(), "no valid commit groups") {
		t.Errorf("expected error about no valid commit groups, got: %v", err)
	}
}

func TestCommitService_LLMError(t *testing.T) {
	llmErr := errors.New("LLM unavailable")
	gen := &mockCommitGenerator{err: llmErr}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			defaultDiff(),      // per-group execution
		},
		allChangedFiles: []string{"main.go"},
	}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)

	if err == nil {
		t.Fatal("expected error from LLM failure, got nil")
	}
	if !errors.Is(err, llmErr) {
		t.Errorf("expected wrapped llmErr, got: %v", err)
	}
}

func TestCommitService_GitCommitError(t *testing.T) {
	commitErr := errors.New("git commit failed")
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			defaultDiff(),      // per-group execution
		},
		allChangedFiles: []string{"main.go"},
		commitErr:       commitErr,
	}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)

	if err == nil {
		t.Fatal("expected error from git commit failure, got nil")
	}
	if !errors.Is(err, commitErr) {
		t.Errorf("expected wrapped commitErr, got: %v", err)
	}
}
