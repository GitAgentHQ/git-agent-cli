package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/hook"
	"github.com/gitagenthq/git-agent/domain/project"
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
	plan    *commit.CommitPlan
	err     error
	lastReq *commit.PlanRequest
}

func (m *mockCommitPlanner) Plan(_ context.Context, req commit.PlanRequest) (*commit.CommitPlan, error) {
	m.lastReq = &req
	return m.plan, m.err
}

type mockCommitGitClient struct {
	stagedDiff            *diff.StagedDiff
	stagedDiffSeq         []*diff.StagedDiff // if set, returned in order; falls back to stagedDiff
	stagedErr             error
	unstagedDiff          *diff.StagedDiff
	unstagedErr           error
	allChangedFiles       []string
	allChangedFilesErr    error
	allChangedFilesCalled bool
	commitCalled          bool
	commitCount           int
	commitMessage         string
	commitErr             error
	unstageAllCalls       int
	stageFilesCalls       int
	stagedFiles           [][]string // tracks each StageFiles call
	lastCommitDiff        *diff.StagedDiff
	lastCommitDiffErr     error
	amendCalled           bool
	amendMessage          string
	amendErr              error
}

func (m *mockCommitGitClient) StagedDiff(_ context.Context) (*diff.StagedDiff, error) {
	if len(m.stagedDiffSeq) > 0 {
		d := m.stagedDiffSeq[0]
		m.stagedDiffSeq = m.stagedDiffSeq[1:]
		return d, m.stagedErr
	}
	if m.stagedDiff == nil {
		return &diff.StagedDiff{}, m.stagedErr
	}
	return m.stagedDiff, m.stagedErr
}

func (m *mockCommitGitClient) UnstagedDiff(_ context.Context) (*diff.StagedDiff, error) {
	if m.unstagedDiff == nil {
		return &diff.StagedDiff{}, nil
	}
	return m.unstagedDiff, m.unstagedErr
}

func (m *mockCommitGitClient) AllChangedFiles(_ context.Context) ([]string, error) {
	m.allChangedFilesCalled = true
	return m.allChangedFiles, m.allChangedFilesErr
}

func (m *mockCommitGitClient) Commit(_ context.Context, message string) (string, error) {
	m.commitCalled = true
	m.commitCount++
	m.commitMessage = message
	return "", m.commitErr
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

func (m *mockCommitGitClient) RepoRoot(_ context.Context) (string, error) {
	return ".", nil
}

func (m *mockCommitGitClient) LastCommitDiff(_ context.Context) (*diff.StagedDiff, error) {
	if m.lastCommitDiff == nil {
		return &diff.StagedDiff{}, m.lastCommitDiffErr
	}
	return m.lastCommitDiff, m.lastCommitDiffErr
}

func (m *mockCommitGitClient) AmendCommit(_ context.Context, message string) (string, error) {
	m.amendCalled = true
	m.amendMessage = message
	return "", m.amendErr
}

type mockHookExecutor struct {
	result *hook.HookResult
	err    error
}

func (m *mockHookExecutor) Execute(_ context.Context, _ []string, _ hook.HookInput) (*hook.HookResult, error) {
	return m.result, m.err
}

// --- helpers ---

func defaultDiff() *diff.StagedDiff {
	return &diff.StagedDiff{Files: []string{"main.go"}, Content: "+func main(){}", Lines: 1}
}

func defaultMsg() *commit.CommitMessage {
	return &commit.CommitMessage{Title: "feat: add feature", Bullets: []string{"Add feature"}, Explanation: "Test explanation."}
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
	return application.NewCommitService(gen, planner, git, hookExec, nil, nil, nil)
}

// --- tests ---

func TestCommitService_GeneratesAndCommits(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // pre-staged check: nothing pre-staged
			defaultDiff(),      // per-group execution
		},
		allChangedFiles: []string{"main.go"},
	}
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
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			defaultDiff(),      // per-group execution
		},
		allChangedFiles: []string{"main.go"},
	}
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
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			defaultDiff(),      // per-group execution
		},
		allChangedFiles: []string{"main.go"},
	}
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
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			defaultDiff(),      // per-group execution
		},
		allChangedFiles: []string{"main.go"},
	}
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
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			defaultDiff(),      // per-group execution (hook retry 1)
			defaultDiff(),      // per-group execution (hook retry 2)
			defaultDiff(),      // per-group execution (hook retry 3)
		},
		allChangedFiles: []string{"main.go"},
	}
	blockingHook := &mockHookExecutor{result: &hook.HookResult{ExitCode: 1, Stderr: "blocked"}}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"main.go"})}
	svc := application.NewCommitService(gen, planner, git, blockingHook, nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{Hooks: []string{"conventional"}}}
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
	// User pre-staged main.go.
	preStagedDiff := &diff.StagedDiff{Files: []string{"main.go"}, Content: "+func main(){}", Lines: 1}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			preStagedDiff, // pre-staged check: user has main.go staged
			&diff.StagedDiff{Files: []string{"main.go"}, Content: "+func main(){}", Lines: 1}, // group 1 (main.go)
			&diff.StagedDiff{Files: []string{"b.go", "c.go"}, Content: "+b+c", Lines: 2},      // group 2 (b.go, c.go)
		},
		allChangedFiles: []string{"main.go", "b.go", "c.go"},
	}

	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"main.go"}},
			{Files: []string{"b.go", "c.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(result.Commits))
	}
}

func TestCommitService_AllChangedFiles_PropagatesError(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiff:         &diff.StagedDiff{},
		allChangedFilesErr: errors.New("git diff exploded"),
	}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when AllChangedFiles fails, got nil")
	}
	if !strings.Contains(err.Error(), "git diff exploded") {
		t.Errorf("expected wrapped error from AllChangedFiles, got: %v", err)
	}
	if git.commitCalled {
		t.Fatal("git.Commit must NOT be called when AllChangedFiles fails")
	}
}

func TestCommitService_AllPreStaged_UnstagedEmpty(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	preStaged := &diff.StagedDiff{Files: []string{"a.go", "b.go"}, Content: "+a+b", Lines: 2}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			preStaged, // pre-staged check: user has both files staged
			&diff.StagedDiff{Files: []string{"a.go", "b.go"}, Content: "+a+b", Lines: 2}, // group execution
		},
		allChangedFiles: []string{"a.go", "b.go"},
	}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"a.go", "b.go"})}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if planner.lastReq == nil {
		t.Fatal("planner was not invoked")
	}
	if got := planner.lastReq.StagedDiff.Files; len(got) != 2 {
		t.Errorf("expected 2 staged files passed to planner, got %v", got)
	}
	if got := planner.lastReq.UnstagedDiff.Files; len(got) != 0 {
		t.Errorf("expected unstaged file list to be empty when all files are pre-staged, got %v", got)
	}
}

func TestCommitService_StagedUnstagedAndUntracked(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	preStaged := &diff.StagedDiff{Files: []string{"a.go"}, Content: "+a", Lines: 1}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			preStaged, // user pre-staged a.go
			&diff.StagedDiff{Files: []string{"a.go"}, Content: "+a", Lines: 1},             // group 1 (a.go)
			&diff.StagedDiff{Files: []string{"b.go", "new.go"}, Content: "+b+n", Lines: 2}, // group 2 (b.go modified + new.go untracked)
		},
		// AllChangedFiles returns staged + unstaged + untracked, deduplicated.
		allChangedFiles: []string{"a.go", "b.go", "new.go"},
	}
	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"a.go"}},
			{Files: []string{"b.go", "new.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if planner.lastReq == nil {
		t.Fatal("planner was not invoked")
	}
	stagedFiles := planner.lastReq.StagedDiff.Files
	if len(stagedFiles) != 1 || stagedFiles[0] != "a.go" {
		t.Errorf("expected planner staged=[a.go], got %v", stagedFiles)
	}
	unstagedFiles := planner.lastReq.UnstagedDiff.Files
	if len(unstagedFiles) != 2 {
		t.Errorf("expected planner unstaged to contain both b.go and new.go, got %v", unstagedFiles)
	}
	seen := map[string]bool{}
	for _, f := range unstagedFiles {
		seen[f] = true
	}
	if !seen["b.go"] || !seen["new.go"] {
		t.Errorf("expected unstaged to include b.go and new.go, got %v", unstagedFiles)
	}
	if len(result.Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(result.Commits))
	}
}

func TestCommitService_AllChangedFiles_ListsUntracked(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			&diff.StagedDiff{Files: []string{"main.go", "new_file.go"}, Content: "+main+new", Lines: 2}, // per-group execution
		},
		allChangedFiles: []string{"main.go", "new_file.go"},
	}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Config: &project.Config{}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !git.allChangedFilesCalled {
		t.Fatal("expected git.AllChangedFiles to be called")
	}
}

func TestCommitService_StagesFilesPerGroup(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			&diff.StagedDiff{Files: []string{"a.go"}, Content: "+a", Lines: 1}, // group 1
			&diff.StagedDiff{Files: []string{"b.go"}, Content: "+b", Lines: 1}, // group 2
		},
		allChangedFiles: []string{"a.go", "b.go"},
	}

	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"a.go"}},
			{Files: []string{"b.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

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

func TestCommitService_NoStage_UsesExistingStaging(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{NoStage: true, Config: &project.Config{}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if git.allChangedFilesCalled {
		t.Fatal("git.AllChangedFiles must NOT be called when --no-stage is set")
	}
	if !git.commitCalled {
		t.Fatal("expected git.Commit to be called")
	}
}

func TestCommitService_NoStage_NothingStaged_ReturnsError(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: &diff.StagedDiff{}}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{NoStage: true, Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when no staged changes with --no-stage, got nil")
	}
	if !strings.Contains(err.Error(), "no staged changes") {
		t.Errorf("expected 'no staged changes' in error, got: %v", err)
	}
}

func TestCommitService_Amend_CallsAmendCommit(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		lastCommitDiff: defaultDiff(),
	}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Amend: true, Config: &project.Config{}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !git.amendCalled {
		t.Fatal("expected git.AmendCommit to be called")
	}
	if git.commitCalled {
		t.Fatal("git.Commit must NOT be called when --amend is set")
	}
	if !strings.Contains(git.amendMessage, "feat: add feature") {
		t.Errorf("amend message missing title, got: %q", git.amendMessage)
	}
}

func TestCommitService_CapCommitGroups(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}

	allFiles := make([]string, 8)
	for i := range allFiles {
		allFiles[i] = fmt.Sprintf("file%d.go", i)
	}

	// Build stagedDiffSeq: pre-staged check (empty) + 5 per-group diffs (capped to 5 groups).
	groupDiffSeq := []*diff.StagedDiff{
		&diff.StagedDiff{}, // nothing pre-staged
	}
	for i := 0; i < 5; i++ {
		groupDiffSeq = append(groupDiffSeq, &diff.StagedDiff{
			Files:   []string{fmt.Sprintf("file%d.go", i)},
			Content: fmt.Sprintf("+file%d", i),
			Lines:   1,
		})
	}

	git := &mockCommitGitClient{
		stagedDiffSeq:   groupDiffSeq,
		allChangedFiles: allFiles,
	}

	groups := make([]commit.CommitGroup, 8)
	for i := range groups {
		groups[i] = commit.CommitGroup{Files: []string{fmt.Sprintf("file%d.go", i)}}
	}
	planner := &mockCommitPlanner{plan: &commit.CommitPlan{Groups: groups}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Commits) != 5 {
		t.Errorf("expected 5 commits (capped), got %d", len(result.Commits))
	}

	// All 8 files must appear across commits: capped groups are recovered into group[0].
	allCommittedFiles := make(map[string]bool)
	for _, c := range result.Commits {
		for _, f := range c.Files {
			allCommittedFiles[f] = true
		}
	}
	for i := 0; i < 8; i++ {
		f := fmt.Sprintf("file%d.go", i)
		if !allCommittedFiles[f] {
			t.Errorf("file %s was not committed — capped group recovery failed", f)
		}
	}
}

func TestCommitService_Amend_NoPreviousCommit_ReturnsError(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		lastCommitDiff: &diff.StagedDiff{},
	}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{Amend: true, Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when no previous commit to amend, got nil")
	}
}

// recordingGenerator captures each GenerateRequest for later inspection.
type recordingGenerator struct {
	reqs []*commit.GenerateRequest
	msgs []*commit.CommitMessage
}

func (r *recordingGenerator) Generate(_ context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error) {
	r.reqs = append(r.reqs, &req)
	idx := len(r.reqs) - 1
	if idx < len(r.msgs) {
		return r.msgs[idx], nil
	}
	return r.msgs[len(r.msgs)-1], nil
}

// sequenceHookExecutor returns results in order, then repeats the last one.
type sequenceHookExecutor struct {
	results []*hook.HookResult
}

func (s *sequenceHookExecutor) Execute(_ context.Context, _ []string, _ hook.HookInput) (*hook.HookResult, error) {
	if len(s.results) == 0 {
		return &hook.HookResult{ExitCode: 0}, nil
	}
	r := s.results[0]
	if len(s.results) > 1 {
		s.results = s.results[1:]
	}
	return r, nil
}

func TestCommitService_HookRetry_SendsPreviousMessage(t *testing.T) {
	msg1 := &commit.CommitMessage{Title: "feat(cli): a very long title that exceeds the fifty character limit", Bullets: []string{"Add feature"}, Explanation: "Test."}
	msg2 := &commit.CommitMessage{Title: "feat(cli): short title", Bullets: []string{"Add feature"}, Explanation: "Test."}

	gen := &recordingGenerator{msgs: []*commit.CommitMessage{msg1, msg2}}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			defaultDiff(),      // per-group execution
		},
		allChangedFiles: []string{"main.go"},
	}
	hookSeq := &sequenceHookExecutor{results: []*hook.HookResult{
		{ExitCode: 1, Stderr: "error: title must be 50 characters or less"},
		{ExitCode: 0},
	}}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"main.go"})}
	svc := application.NewCommitService(gen, planner, git, hookSeq, nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{Hooks: []string{"conventional"}}}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gen.reqs) != 2 {
		t.Fatalf("expected 2 Generate calls, got %d", len(gen.reqs))
	}
	if gen.reqs[0].PreviousMessage != "" {
		t.Errorf("attempt 1: expected PreviousMessage empty, got %q", gen.reqs[0].PreviousMessage)
	}
	if !strings.Contains(gen.reqs[1].PreviousMessage, msg1.Title) {
		t.Errorf("attempt 2: expected PreviousMessage to contain %q, got %q", msg1.Title, gen.reqs[1].PreviousMessage)
	}
	if gen.reqs[1].HookFeedback == "" {
		t.Error("attempt 2: expected HookFeedback to be set")
	}
}

func TestCommitService_SkipsGroupWithEmptyDiff(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	nonEmpty := &diff.StagedDiff{Files: []string{"a.go"}, Content: "+a", Lines: 1}
	empty := &diff.StagedDiff{}

	git := &mockCommitGitClient{
		// NoStage=true: call 1 (initial check), then call 2 (group 0), call 3 (group 1).
		stagedDiffSeq: []*diff.StagedDiff{nonEmpty, nonEmpty, empty},
	}

	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"a.go"}},
			{Files: []string{"stale.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{NoStage: true, Config: &project.Config{}}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Commits) != 1 {
		t.Errorf("expected 1 commit (empty group skipped), got %d", len(result.Commits))
	}
	if git.commitCount != 1 {
		t.Errorf("expected git.Commit called once, got %d", git.commitCount)
	}
}

func TestCommitService_RestagesOnCommitError(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}

	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			&diff.StagedDiff{}, // nothing pre-staged
			&diff.StagedDiff{Files: []string{"a.go"}, Content: "+a", Lines: 1}, // group 1 execution
		},
		allChangedFiles: []string{"a.go", "b.go"},
		commitErr:       fmt.Errorf("simulated commit failure"),
	}

	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"a.go"}},
			{Files: []string{"b.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil)

	req := application.CommitRequest{Config: &project.Config{}}
	_, err := svc.Commit(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from commit failure")
	}

	// StageFiles: call 1 = group 0 staging, call 2 = recovery re-stage.
	if git.stageFilesCalls < 2 {
		t.Fatalf("expected recovery StageFiles call, got %d total calls", git.stageFilesCalls)
	}

	// Recovery call should re-stage all files since no commits succeeded.
	recoveryFiles := make(map[string]bool)
	for _, f := range git.stagedFiles[git.stageFilesCalls-1] {
		recoveryFiles[f] = true
	}
	if !recoveryFiles["a.go"] || !recoveryFiles["b.go"] {
		t.Errorf("expected recovery to re-stage [a.go, b.go], got %v", git.stagedFiles[git.stageFilesCalls-1])
	}
}
