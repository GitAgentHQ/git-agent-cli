package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fradser/ga-cli/application"
	"github.com/fradser/ga-cli/domain/commit"
	"github.com/fradser/ga-cli/domain/diff"
	"github.com/fradser/ga-cli/domain/hook"
	"github.com/fradser/ga-cli/domain/project"
)

// --- mocks ---

type mockCommitGenerator struct {
	msg *commit.CommitMessage
	err error
}

func (m *mockCommitGenerator) Generate(_ context.Context, _ commit.GenerateRequest) (*commit.CommitMessage, error) {
	return m.msg, m.err
}

type mockCommitGitClient struct {
	stagedDiff    *diff.StagedDiff
	stagedErr     error
	commitCalled  bool
	commitMessage string
	commitErr     error
	addAllCalled  bool
	addAllErr     error
}

func (m *mockCommitGitClient) StagedDiff(_ context.Context) (*diff.StagedDiff, error) {
	return m.stagedDiff, m.stagedErr
}

func (m *mockCommitGitClient) Commit(_ context.Context, message string) error {
	m.commitCalled = true
	m.commitMessage = message
	return m.commitErr
}

func (m *mockCommitGitClient) AddAll(_ context.Context) error {
	m.addAllCalled = true
	return m.addAllErr
}

type mockHookExecutor struct {
	result *hook.HookResult
	err    error
}

func (m *mockHookExecutor) Execute(_ context.Context, _ string, _ hook.HookInput) (*hook.HookResult, error) {
	return m.result, m.err
}

// --- helpers ---

func defaultDiff() *diff.StagedDiff {
	return &diff.StagedDiff{Files: []string{"main.go"}, Content: "+func main(){}", Lines: 1}
}

func defaultMsg() *commit.CommitMessage {
	return &commit.CommitMessage{Title: "feat: add feature", Body: "body text"}
}

func noopHook() *mockHookExecutor {
	return &mockHookExecutor{result: &hook.HookResult{ExitCode: 0}}
}

// --- tests ---

func TestCommitService_GeneratesAndCommits(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := application.NewCommitService(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	if err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !git.commitCalled {
		t.Fatal("expected git.Commit to be called")
	}
	if !strings.Contains(git.commitMessage, "feat: add feature") {
		t.Errorf("commit message missing title, got: %q", git.commitMessage)
	}
}

func TestCommitService_DryRun(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := application.NewCommitService(gen, git, noopHook())

	req := application.CommitRequest{DryRun: true, Config: &project.Config{}}
	if err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if git.commitCalled {
		t.Fatal("git.Commit must NOT be called in dry-run mode")
	}
}

func TestCommitService_CoAuthor(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := application.NewCommitService(gen, git, noopHook())

	req := application.CommitRequest{CoAuthor: "Alice <alice@example.com>", Config: &project.Config{}}
	if err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(git.commitMessage, "Co-Authored-By: Alice <alice@example.com>") {
		t.Errorf("commit message missing co-author footer, got: %q", git.commitMessage)
	}
}

func TestCommitService_AllFlag(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := application.NewCommitService(gen, git, noopHook())

	req := application.CommitRequest{All: true, Config: &project.Config{}}
	if err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !git.addAllCalled {
		t.Fatal("expected git.AddAll to be called when All=true")
	}
}

func TestCommitService_HookBlocks(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	blockingHook := &mockHookExecutor{result: &hook.HookResult{ExitCode: 1, Stderr: "blocked"}}
	svc := application.NewCommitService(gen, git, blockingHook)

	req := application.CommitRequest{HookPath: "/some/hook", Config: &project.Config{}}
	err := svc.Commit(context.Background(), req)

	if err == nil {
		t.Fatal("expected error when hook blocks, got nil")
	}
	if !errors.Is(err, application.ErrHookBlocked) && !strings.Contains(err.Error(), "hook") {
		t.Errorf("expected hook-related error, got: %v", err)
	}
	if git.commitCalled {
		t.Fatal("git.Commit must NOT be called when hook blocks")
	}
}
