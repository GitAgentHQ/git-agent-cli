package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/hook"
	"github.com/gitagenthq/git-agent/domain/project"
	infraDiff "github.com/gitagenthq/git-agent/infrastructure/diff"
)

// --- mocks ---

type mockCommitGenerator struct {
	msg     *commit.CommitMessage
	err     error
	lastReq *commit.GenerateRequest
}

func (m *mockCommitGenerator) Generate(_ context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error) {
	m.lastReq = &req
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
	// Per-test overrides for the DIFF-SYNOPSIS fallback path. When stagedDiffStat
	// is non-nil it is invoked instead of returning the default ("", nil); tests
	// that exercise REQ-007 use this to supply canned stat output and count
	// invocations.
	stagedDiffStat      func(ctx context.Context) (string, error)
	stagedDiffStatCalls int
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

func (m *mockCommitGitClient) StagedDiffStat(ctx context.Context) (string, error) {
	m.stagedDiffStatCalls++
	if m.stagedDiffStat != nil {
		return m.stagedDiffStat(ctx)
	}
	return "", nil
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
	result    *hook.HookResult
	err       error
	lastInput hook.HookInput
	inputs    []hook.HookInput
}

func (m *mockHookExecutor) Execute(_ context.Context, _ []string, input hook.HookInput) (*hook.HookResult, error) {
	m.lastInput = input
	m.inputs = append(m.inputs, input)
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
	return application.NewCommitService(gen, planner, git, hookExec, nil, nil, nil, nil)
}

// --- tests ---

func TestCommitService_ByteCapAppliedBeforeGenerate(t *testing.T) {
	// A multi-file blob far over the request-body limit: the line cap cannot
	// catch it, so the always-on byte cap must shrink the diff before it
	// reaches the generator. Multi-file keeps the synopsis fallback (REQ-007)
	// out of the picture so we exercise the truncator path specifically.
	huge := strings.Repeat("x", application.DefaultMaxDiffBytes*2)
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiff: &diff.StagedDiff{Files: []string{"vendor.min.js", "vendor.min.css"}, Content: huge, Lines: 1},
	}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"vendor.min.js", "vendor.min.css"})}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, infraDiff.NewLineTruncator(), nil)

	_, err := svc.Commit(context.Background(), application.CommitRequest{
		NoStage: true,
		Config:  &project.Config{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gen.lastReq == nil {
		t.Fatal("generator was not called")
	}
	n := len(gen.lastReq.Diff.Content)
	if n == 0 {
		t.Fatal("byte cap truncated the diff to empty")
	}
	if n > application.DefaultMaxDiffBytes {
		t.Fatalf("diff sent to LLM is %d bytes, exceeds cap %d", n, application.DefaultMaxDiffBytes)
	}
	// All-ASCII input: the cap should keep right up to the budget, not seek back
	// to an earlier boundary and discard the bulk of the diff.
	if n < application.DefaultMaxDiffBytes-utf8.UTFMax {
		t.Fatalf("byte cap over-truncated: kept only %d of %d bytes", n, application.DefaultMaxDiffBytes)
	}
}

func TestCommitService_AmendByteCap_MultibyteContent(t *testing.T) {
	// Covers two gaps: the amend path's byte-cap branch (commitAmend uses
	// LastCommitDiff, not StagedDiff) and the multi-byte rune-trim path (the
	// other byte-cap test uses pure ASCII so the trim loop never fires).
	huge := strings.Repeat("世", application.DefaultMaxDiffBytes) // 3*N bytes >> cap
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		lastCommitDiff: &diff.StagedDiff{Files: []string{"chinese.md"}, Content: huge, Lines: 1},
	}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"chinese.md"})}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, infraDiff.NewLineTruncator(), nil)

	_, err := svc.Commit(context.Background(), application.CommitRequest{
		Amend:  true,
		Config: &project.Config{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gen.lastReq == nil {
		t.Fatal("generator was not called")
	}
	n := len(gen.lastReq.Diff.Content)
	if n == 0 {
		t.Fatal("amend byte cap truncated the diff to empty")
	}
	if n > application.DefaultMaxDiffBytes {
		t.Fatalf("amend diff sent to LLM is %d bytes, exceeds cap %d", n, application.DefaultMaxDiffBytes)
	}
	if !utf8.ValidString(gen.lastReq.Diff.Content) {
		t.Fatal("truncated amend diff is not valid UTF-8 — mid-rune cut not repaired")
	}
	// "世" is 3 bytes, so the largest valid prefix under the cap is the
	// nearest multiple of 3 to maxBytes.
	want := (application.DefaultMaxDiffBytes / 3) * 3
	if n != want {
		t.Fatalf("expected %d bytes (largest multiple of 3 ≤ cap), got %d", want, n)
	}
}

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
	svc := application.NewCommitService(gen, planner, git, blockingHook, nil, nil, nil, nil)

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
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

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
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

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
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

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
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

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
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

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
	svc := application.NewCommitService(gen, planner, git, hookSeq, nil, nil, nil, nil)

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
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

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

// TestCommitService_AlwaysOnPhaseLines covers both halves of the hotfix
// contract for OutWriter:
//
//  1. With a non-nil writer (interactive terminal or --verbose), phase lines
//     are emitted and never leak diff content into the always-on stream.
//  2. With a nil writer (the post-hotfix non-TTY default that the cmd layer
//     passes), no phase line reaches stderr at all — agent / pipe consumers
//     get a quiet stream unless they opt in.
func TestCommitService_AlwaysOnPhaseLines(t *testing.T) {
	build := func() (*mockCommitGenerator, *mockCommitGitClient, *mockCommitPlanner) {
		gen := &mockCommitGenerator{msg: defaultMsg()}
		git := &mockCommitGitClient{
			stagedDiffSeq: []*diff.StagedDiff{
				{}, // pre-staged check: nothing pre-staged
				{Files: []string{"a.go"}, Content: "+a // sentinel-diff-content", Lines: 1}, // group 1
				{Files: []string{"b.go"}, Content: "+b // sentinel-diff-content", Lines: 1}, // group 2
			},
			allChangedFiles: []string{"a.go", "b.go"},
		}
		planner := &mockCommitPlanner{plan: &commit.CommitPlan{
			Groups: []commit.CommitGroup{
				{Files: []string{"a.go"}},
				{Files: []string{"b.go"}},
			},
		}}
		return gen, git, planner
	}

	t.Run("non_nil_writer_emits_phase_lines", func(t *testing.T) {
		gen, git, planner := build()
		svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

		var out bytes.Buffer
		req := application.CommitRequest{
			Config:    &project.Config{},
			Verbose:   false,
			OutWriter: &out,
		}
		if _, err := svc.Commit(context.Background(), req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got := out.String()
		for _, want := range []string{
			"planning commits...",
			"planned 2 commit(s)",
			"commit 1/2: drafting message (attempt 1/3)",
			"commit 2/2: drafting message (attempt 1/3)",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("always-on stderr missing %q\ngot:\n%s", want, got)
			}
		}
		if strings.Contains(got, "sentinel-diff-content") {
			t.Errorf("always-on stderr leaked diff content:\n%s", got)
		}
	})

	t.Run("nil_writer_emits_nothing", func(t *testing.T) {
		gen, git, planner := build()
		svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

		// Nil OutWriter: the cmd layer passes nil on non-TTY paths so
		// agents and pipes get a quiet stderr. The service must not panic
		// AND must not attempt to write phase lines anywhere.
		req := application.CommitRequest{
			Config:    &project.Config{},
			Verbose:   false,
			OutWriter: nil,
		}
		if _, err := svc.Commit(context.Background(), req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Sanity: commit still happened. (Lack of panic above is the
		// primary assertion; if s.out grew a regression that dereferenced
		// nil, the run would already have failed.)
		if !git.commitCalled {
			t.Fatal("expected git.Commit to be called even with nil OutWriter")
		}
	})
}

// TestCommitService_PhaseLinesNoSecretLeakage locks in REQ-011 for the
// application layer: the always-on phase stream (OutWriter) emits only
// progress metadata. Even when both the user intent and the diff content
// contain a sentinel string, the captured always-on buffer must contain
// zero occurrences of it. Verbose-only output (LogWriter) is excluded from
// this guard — REQ-011 covers the always-on stream only.
func TestCommitService_PhaseLinesNoSecretLeakage(t *testing.T) {
	const sentinel = "SECRET-DIFF-CONTENT-NEVER-LOG"
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			{}, // pre-staged check: nothing pre-staged
			{Files: []string{"a.go"}, Content: "+a // " + sentinel, Lines: 1}, // group 1
			{Files: []string{"b.go"}, Content: "+b // " + sentinel, Lines: 1}, // group 2
		},
		allChangedFiles: []string{"a.go", "b.go"},
	}
	planner := &mockCommitPlanner{plan: &commit.CommitPlan{
		Groups: []commit.CommitGroup{
			{Files: []string{"a.go"}},
			{Files: []string{"b.go"}},
		},
	}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

	var out bytes.Buffer
	req := application.CommitRequest{
		Intent:    sentinel,
		Config:    &project.Config{},
		OutWriter: &out,
	}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(out.String(), sentinel) {
		t.Errorf("always-on (OutWriter) leaked sentinel %q:\n%s", sentinel, out.String())
	}
}

func TestCommitService_VerboseIsSuperset(t *testing.T) {
	run := func(verbose bool) string {
		gen := &mockCommitGenerator{msg: defaultMsg()}
		git := &mockCommitGitClient{
			stagedDiffSeq: []*diff.StagedDiff{
				{}, // pre-staged check
				{Files: []string{"a.go"}, Content: "+a", Lines: 1}, // group 1
				{Files: []string{"b.go"}, Content: "+b", Lines: 1}, // group 2
			},
			allChangedFiles: []string{"a.go", "b.go"},
		}
		planner := &mockCommitPlanner{plan: &commit.CommitPlan{
			Groups: []commit.CommitGroup{
				{Files: []string{"a.go"}},
				{Files: []string{"b.go"}},
			},
		}}
		svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

		var buf bytes.Buffer
		req := application.CommitRequest{
			Config:    &project.Config{},
			Verbose:   verbose,
			LogWriter: &buf,
			OutWriter: &buf,
		}
		if _, err := svc.Commit(context.Background(), req); err != nil {
			t.Fatalf("unexpected error (verbose=%v): %v", verbose, err)
		}
		return buf.String()
	}

	always := run(false)
	verbose := run(true)

	// Every line in always-on appears exactly once in verbose, and exactly
	// once in always (no duplication within either stream).
	alwaysLines := splitNonEmptyLines(always)
	verboseLines := splitNonEmptyLines(verbose)

	alwaysCount := countLines(alwaysLines)
	verboseCount := countLines(verboseLines)
	// Always-on stream: every line is unique (phase markers, not per-group
	// chatter). Per-group lines vary by index so they remain unique here.
	for line, n := range alwaysCount {
		if n != 1 {
			t.Errorf("always-on stream duplicated line %q (n=%d)", line, n)
		}
	}
	// Verbose stream is a superset: every always-on line appears exactly
	// once (same uniqueness as above — verbose adds debug lines, not
	// duplicates of the phase markers).
	for _, line := range alwaysLines {
		if verboseCount[line] != 1 {
			t.Errorf("verbose stream missing always-on line %q (count=%d)\nverbose:\n%s",
				line, verboseCount[line], verbose)
		}
	}

	// Verbose stream contributes additional verbose-only lines, including
	// the file lists.
	hasStaged := false
	hasUnstaged := false
	for _, line := range verboseLines {
		if strings.HasPrefix(line, "staged files:") {
			hasStaged = true
		}
		if strings.HasPrefix(line, "unstaged files:") {
			hasUnstaged = true
		}
	}
	if !hasStaged {
		t.Errorf("verbose stream missing 'staged files:'\n%s", verbose)
	}
	if !hasUnstaged {
		t.Errorf("verbose stream missing 'unstaged files:'\n%s", verbose)
	}
}

func splitNonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func countLines(lines []string) map[string]int {
	out := make(map[string]int, len(lines))
	for _, line := range lines {
		out[line]++
	}
	return out
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
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, nil)

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

// TestCommitService_SynopsisFallbackOneFile exercises REQ-007: when a single
// staged file's diff saturates the byte cap, the LLM prompt is replaced with a
// compact DIFF-SYNOPSIS block. The hook still receives the full unstripped
// diff so its validation surface is unchanged.
func TestCommitService_SynopsisFallbackOneFile(t *testing.T) {
	const maxBytes = 393216
	const actualBytes = 1048576
	huge := strings.Repeat("x", actualBytes)
	statLine := " vendored/bundle.min.js | 12345 +++++++++++++++++++++++++++++++++++"

	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			{}, // pre-staged check: nothing pre-staged
			{Files: []string{"vendored/bundle.min.js"}, Content: huge, Lines: 1}, // per-group execution
		},
		allChangedFiles: []string{"vendored/bundle.min.js"},
		stagedDiffStat: func(_ context.Context) (string, error) {
			return statLine, nil
		},
	}
	planner := &mockCommitPlanner{plan: singleGroupPlan([]string{"vendored/bundle.min.js"})}
	hookExec := &mockHookExecutor{result: &hook.HookResult{ExitCode: 0}}
	svc := application.NewCommitService(gen, planner, git, hookExec, nil, nil, infraDiff.NewLineTruncator(), nil)

	var out bytes.Buffer
	req := application.CommitRequest{
		Config:    &project.Config{Hooks: []string{"conventional"}},
		MaxBytes:  maxBytes,
		OutWriter: &out,
	}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gen.lastReq == nil {
		t.Fatal("generator was not called")
	}
	content := gen.lastReq.Diff.Content
	if !strings.HasPrefix(content, "DIFF-SYNOPSIS") {
		t.Fatalf("expected synopsis prefix, got: %q", content[:min(64, len(content))])
	}
	for _, want := range []string{
		"file: vendored/bundle.min.js",
		"changes: +12345 / -0 (stat)",
		"note: full diff elided (1048576 bytes exceeded 393216-byte cap)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("synopsis missing line %q\ngot:\n%s", want, content)
		}
	}
	if len(content) >= 4096 {
		t.Errorf("synopsis is %d bytes, expected under 4096", len(content))
	}

	wantPhase := "commit 1/1: DIFF-SYNOPSIS fallback (vendored/bundle.min.js)"
	if !strings.Contains(out.String(), wantPhase) {
		t.Errorf("phase output missing %q\ngot:\n%s", wantPhase, out.String())
	}

	if hookExec.lastInput.Diff != huge {
		t.Errorf("hook diff was %d bytes, expected full %d-byte raw diff", len(hookExec.lastInput.Diff), actualBytes)
	}

	if !git.commitCalled {
		t.Fatal("expected commit to succeed")
	}
}

// TestCommitService_TruncatorPathMultiFile asserts the synopsis is NOT used
// when the saturated diff spans more than one file: the truncator path stays
// in force and StagedDiffStat is not called for that group.
func TestCommitService_TruncatorPathMultiFile(t *testing.T) {
	const maxBytes = 393216
	const totalBytes = 500000
	groupContent := strings.Repeat("y", totalBytes)
	files := []string{"a.go", "b.go", "c.go", "d.go"}

	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			{}, // pre-staged check
			{Files: files, Content: groupContent, Lines: 4},
		},
		allChangedFiles: files,
	}
	planner := &mockCommitPlanner{plan: singleGroupPlan(files)}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, infraDiff.NewLineTruncator(), nil)

	var out bytes.Buffer
	req := application.CommitRequest{
		Config:    &project.Config{},
		MaxBytes:  maxBytes,
		OutWriter: &out,
	}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gen.lastReq == nil {
		t.Fatal("generator was not called")
	}
	if strings.HasPrefix(gen.lastReq.Diff.Content, "DIFF-SYNOPSIS") {
		t.Error("multi-file path must NOT emit DIFF-SYNOPSIS")
	}

	wantPhase := fmt.Sprintf("commit 1/1: truncating group diff (%d bytes)", maxBytes)
	if !strings.Contains(out.String(), wantPhase) {
		t.Errorf("phase output missing %q\ngot:\n%s", wantPhase, out.String())
	}

	if git.stagedDiffStatCalls != 0 {
		t.Errorf("StagedDiffStat must not be called for multi-file groups, got %d calls", git.stagedDiffStatCalls)
	}
}

// spyHeuristicPlanner records every Plan invocation and returns a canned plan.
type spyHeuristicPlanner struct {
	calls int
	plan  *commit.CommitPlan
}

func (s *spyHeuristicPlanner) Plan(_ context.Context, _ commit.PlanRequest) (*commit.CommitPlan, error) {
	s.calls++
	return s.plan, nil
}

// TestCommitService_HeuristicFallback_OptIn exercises the explicit opt-in:
// when the LLM planner returns ErrPlannerBudgetExhausted and PlanFallback is
// either "heuristic" (legacy keyword) OR unset (post-hotfix default), the
// heuristic planner is invoked and the per-group loop proceeds as normal.
func TestCommitService_HeuristicFallback_OptIn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fallback string
	}{
		{name: "explicit_heuristic", fallback: project.PlanFallbackHeuristic},
		{name: "default_empty", fallback: ""},
		{name: "explicit_auto", fallback: project.PlanFallbackAuto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := &mockCommitGenerator{msg: defaultMsg()}
			git := &mockCommitGitClient{
				stagedDiffSeq: []*diff.StagedDiff{
					{}, // pre-staged check
					{Files: []string{"cmd/a.go"}, Content: "+a", Lines: 1},
					{Files: []string{"application/b.go"}, Content: "+b", Lines: 1},
				},
				allChangedFiles: []string{"cmd/a.go", "application/b.go"},
			}
			planner := &mockCommitPlanner{err: &commit.PlannerBudgetExhaustedError{Model: "test-model", Ceiling: 16384}}
			heuristic := &spyHeuristicPlanner{plan: &commit.CommitPlan{Groups: []commit.CommitGroup{
				{Files: []string{"cmd/a.go"}, Message: commit.CommitMessage{Title: "chore(cli): update cmd/"}},
				{Files: []string{"application/b.go"}, Message: commit.CommitMessage{Title: "chore(app): update application/"}},
			}}}
			svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, heuristic)

			var out bytes.Buffer
			req := application.CommitRequest{
				Config:    &project.Config{PlanFallback: tc.fallback},
				OutWriter: &out,
			}
			result, err := svc.Commit(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if heuristic.calls != 1 {
				t.Errorf("expected heuristic planner called once, got %d", heuristic.calls)
			}
			if len(result.Commits) != 2 {
				t.Errorf("expected 2 commits via heuristic plan, got %d", len(result.Commits))
			}
			wantPhase := "planner unavailable (budget exhausted) — falling back to directoryBucketer"
			if !strings.Contains(out.String(), wantPhase) {
				t.Errorf("phase output missing %q\ngot:\n%s", wantPhase, out.String())
			}
		})
	}
}

// TestCommitService_HeuristicFallback_OptOut exercises the explicit opt-out:
// when PlanFallback is "none" the budget-exhausted error propagates unchanged
// and the heuristic planner is never consulted, preserving the loud-failure
// path for advanced users.
func TestCommitService_HeuristicFallback_OptOut(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq:   []*diff.StagedDiff{{}},
		allChangedFiles: []string{"cmd/a.go", "application/b.go"},
	}
	planner := &mockCommitPlanner{err: &commit.PlannerBudgetExhaustedError{Model: "test-model", Ceiling: 16384}}
	heuristic := &spyHeuristicPlanner{plan: &commit.CommitPlan{Groups: []commit.CommitGroup{{Files: []string{"cmd/a.go"}}}}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, heuristic)

	req := application.CommitRequest{Config: &project.Config{PlanFallback: project.PlanFallbackNone}}
	_, err := svc.Commit(context.Background(), req)
	if err == nil {
		t.Fatal("expected error to propagate when PlanFallback=none")
	}
	if heuristic.calls != 0 {
		t.Errorf("expected heuristic planner NOT called, got %d", heuristic.calls)
	}
	if !errors.Is(err, commit.ErrPlannerBudgetExhausted) {
		t.Errorf("expected wrapped ErrPlannerBudgetExhausted, got: %v", err)
	}
}

// TestCommitService_AutoFallbackOnTimeout covers the hotfix: when the LLM
// planner returns a *commit.PlannerTimedOutError, the default (or "auto")
// fallback behaviour routes the request through the heuristic planner
// instead of bubbling the timeout up.
func TestCommitService_AutoFallbackOnTimeout(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq: []*diff.StagedDiff{
			{}, // pre-staged check
			{Files: []string{"cmd/a.go"}, Content: "+a", Lines: 1},
			{Files: []string{"application/b.go"}, Content: "+b", Lines: 1},
		},
		allChangedFiles: []string{"cmd/a.go", "application/b.go"},
	}
	planner := &mockCommitPlanner{err: &commit.PlannerTimedOutError{Model: "qwen3.6-flash", Timeout: 90 * time.Second}}
	heuristic := &spyHeuristicPlanner{plan: &commit.CommitPlan{Groups: []commit.CommitGroup{
		{Files: []string{"cmd/a.go"}, Message: commit.CommitMessage{Title: "chore(cli): update cmd/"}},
		{Files: []string{"application/b.go"}, Message: commit.CommitMessage{Title: "chore(app): update application/"}},
	}}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, heuristic)

	var out bytes.Buffer
	req := application.CommitRequest{
		Config:    &project.Config{}, // default = auto-fallback enabled
		OutWriter: &out,
	}
	result, err := svc.Commit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if heuristic.calls != 1 {
		t.Errorf("expected heuristic planner called once on timeout, got %d", heuristic.calls)
	}
	if len(result.Commits) != 2 {
		t.Errorf("expected 2 commits via heuristic plan, got %d", len(result.Commits))
	}
	wantPhase := "planner unavailable (timed out) — falling back to directoryBucketer"
	if !strings.Contains(out.String(), wantPhase) {
		t.Errorf("phase output missing %q\ngot:\n%s", wantPhase, out.String())
	}
}

// TestCommitService_TimeoutPropagatesWhenOptedOut confirms that the loud-
// failure path still works for the timeout error type when the project has
// explicitly opted out via plan_fallback=none.
func TestCommitService_TimeoutPropagatesWhenOptedOut(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{
		stagedDiffSeq:   []*diff.StagedDiff{{}},
		allChangedFiles: []string{"cmd/a.go", "application/b.go"},
	}
	planner := &mockCommitPlanner{err: &commit.PlannerTimedOutError{Model: "qwen3.6-flash", Timeout: 90 * time.Second}}
	heuristic := &spyHeuristicPlanner{plan: &commit.CommitPlan{Groups: []commit.CommitGroup{{Files: []string{"cmd/a.go"}}}}}
	svc := application.NewCommitService(gen, planner, git, noopHook(), nil, nil, nil, heuristic)

	req := application.CommitRequest{Config: &project.Config{PlanFallback: project.PlanFallbackNone}}
	_, err := svc.Commit(context.Background(), req)
	if err == nil {
		t.Fatal("expected timeout error to propagate when PlanFallback=none")
	}
	if heuristic.calls != 0 {
		t.Errorf("expected heuristic planner NOT called, got %d", heuristic.calls)
	}
	if !errors.Is(err, commit.ErrPlannerTimedOut) {
		t.Errorf("expected wrapped ErrPlannerTimedOut, got: %v", err)
	}
}
