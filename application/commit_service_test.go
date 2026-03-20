package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fradser/git-agent/application"
	"github.com/fradser/git-agent/domain/commit"
	"github.com/fradser/git-agent/domain/diff"
	"github.com/fradser/git-agent/domain/hook"
	"github.com/fradser/git-agent/domain/project"
)

// --- mocks ---

type mockCommitGenerator struct {
	msg *commit.CommitMessage
	err error
}

func (m *mockCommitGenerator) Generate(_ context.Context, _ commit.GenerateRequest) (*commit.CommitMessage, error) {
	return m.msg, m.err
}

type mockCommitPlanner struct {
	plan *commit.CommitPlan
	err  error
}

func (m *mockCommitPlanner) Plan(_ context.Context, _ commit.PlanRequest) (*commit.CommitPlan, error) {
	return m.plan, m.err
}

type mockCommitGitClient struct {
	stagedDiff      *diff.StagedDiff
	stagedErr       error
	unstagedDiff    *diff.StagedDiff
	unstagedErr     error
	commitCalled    bool
	commitMessage   string
	commitErr       error
	addAllCalled    bool
	addAllErr       error
	unstageAllCalls int
	stageFilesCalls int
	stagedFiles     [][]string // tracks each StageFiles call
}

func (m *mockCommitGitClient) StagedDiff(_ context.Context) (*diff.StagedDiff, error) {
	return m.stagedDiff, m.stagedErr
}

func (m *mockCommitGitClient) UnstagedDiff(_ context.Context) (*diff.StagedDiff, error) {
	if m.unstagedDiff == nil {
		return &diff.StagedDiff{}, nil
	}
	return m.unstagedDiff, m.unstagedErr
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

func (m *mockCommitGitClient) UnstageAll(_ context.Context) error {
	m.unstageAllCalls++
	return nil
}

func (m *mockCommitGitClient) StageFiles(_ context.Context, files []string) error {
	m.stageFilesCalls++
	m.stagedFiles = append(m.stagedFiles, files)
	return nil
}

func (m *mockCommitGitClient) FormatTrailers(_ context.Context, message string, trailers []commit.Trailer) (string, error) {
	for _, t := range trailers {
		message += "\n" + t.Key + ": " + t.Value
	}
	return message, nil
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
	return &commit.CommitMessage{Title: "feat: add feature", Body: "body text", Outline: "adds feature"}
}

func noopHook() *mockHookExecutor {
	return &mockHookExecutor{result: &hook.HookResult{ExitCode: 0}}
}

func singleGroupPlan(files []string) *commit.CommitPlan {
	return &commit.CommitPlan{
		Groups: []commit.CommitGroup{{Files: files}},
	}
}

func newSvc(gen *mockCommitGenerator, git *mockCommitGitClient, hookExec *mockHookExecutor) *application.CommitService {
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"main.go"})}
	return application.NewCommitService(gen, planner, git, hookExec, nil)
}

// --- tests ---

func TestCommitService_GeneratesAndCommits(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
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
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{DryRun: true, Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if git.commitCalled {
		t.Fatal("git.Commit must NOT be called in dry-run mode")
	}
	if !result.DryRun {
		t.Error("expected result.DryRun=true")
	}
	if len(result.Commits) == 0 {
		t.Error("expected at least one commit result in dry-run")
	}
}

func TestCommitService_CoAuthor(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{
		Trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Alice <alice@example.com>"}},
		Config:   &project.Config{},
	}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(git.commitMessage, "Co-Authored-By: Alice <alice@example.com>") {
		t.Errorf("commit message missing co-author footer, got: %q", git.commitMessage)
	}
}

func TestCommitService_MixedTrailers(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{
		Trailers: []commit.Trailer{
			{Key: "Co-Authored-By", Value: "Alice <alice@example.com>"},
			{Key: "Signed-off-by", Value: "Bob <bob@example.com>"},
		},
		Config: &project.Config{},
	}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(git.commitMessage, "Co-Authored-By: Alice <alice@example.com>") {
		t.Errorf("commit message missing co-author trailer, got: %q", git.commitMessage)
	}
	if !strings.Contains(git.commitMessage, "Signed-off-by: Bob <bob@example.com>") {
		t.Errorf("commit message missing signed-off-by trailer, got: %q", git.commitMessage)
	}
}

func TestCommitService_HookBlocks(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	blockingHook := &mockHookExecutor{result: &hook.HookResult{ExitCode: 1, Stderr: "blocked"}}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"main.go"})}
	svc := application.NewCommitService(gen, planner, git, blockingHook, nil)

	req := application.CommitRequest{HookPath: "/some/hook", Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)

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

func TestCommitService_MultiCommit_StagedAndUnstaged(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	git.unstagedDiff = &diff.StagedDiff{Files: []string{"b.go", "c.go"}, Content: "+b+c", Lines: 2}

	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"main.go"}},
			{Files: []string{"b.go", "c.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil)

	req := application.CommitRequest{Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(result.Commits))
	}
}

func TestCommitService_UntrackedFiles_AutoStaged(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !git.addAllCalled {
		t.Fatal("expected git.AddAll to be called before diff checks")
	}
}

func TestCommitService_StagesFilesPerGroup(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}

	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"a.go"}},
			{Files: []string{"b.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil)

	req := application.CommitRequest{Config: &project.Config{}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if git.stageFilesCalls != 2 {
		t.Errorf("expected StageFiles called 2 times, got %d", git.stageFilesCalls)
	}
	if git.unstageAllCalls != 2 {
		t.Errorf("expected UnstageAll called 2 times, got %d", git.unstageAllCalls)
	}
}
