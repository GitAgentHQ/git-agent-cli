package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/fradser/git-agent/domain/commit"
	"github.com/fradser/git-agent/domain/diff"
	"github.com/fradser/git-agent/domain/hook"
	"github.com/fradser/git-agent/domain/project"
)

var ErrHookBlocked = errors.New("hook blocked commit")

// SingleCommitResult holds the output of one committed group.
type SingleCommitResult struct {
	Outline string
	Files   []string
	Title   string
}

// CommitResult holds the output of a successful Commit call.
type CommitResult struct {
	Commits []SingleCommitResult
	DryRun  bool
}

type CommitGitClient interface {
	StagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	UnstagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	StageFiles(ctx context.Context, files []string) error
	UnstageAll(ctx context.Context) error
	Commit(ctx context.Context, message string) error
	AddAll(ctx context.Context) error
}

type CommitRequest struct {
	Intent    string
	CoAuthors []string
	HookPath  string
	DryRun    bool
	Config    *project.Config // nil = trigger auto-scope if scopeSvc provided
	MaxLines  int
	Verbose   bool
	LogWriter io.Writer // verbose-only output
	OutWriter io.Writer // always-visible output (hook block context, retries)
}

type CommitService struct {
	gen      commit.CommitMessageGenerator
	planner  commit.CommitPlanner
	git      CommitGitClient
	hookExec hook.HookExecutor
	scopeSvc *ScopeService // nil = no auto-scope
}

func NewCommitService(
	gen commit.CommitMessageGenerator,
	planner commit.CommitPlanner,
	git CommitGitClient,
	hookExec hook.HookExecutor,
	scopeSvc *ScopeService,
) *CommitService {
	return &CommitService{
		gen:      gen,
		planner:  planner,
		git:      git,
		hookExec: hookExec,
		scopeSvc: scopeSvc,
	}
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
const maxRePlans = 2

func (s *CommitService) Commit(ctx context.Context, req CommitRequest) (*CommitResult, error) {
	if err := s.git.AddAll(ctx); err != nil {
		return nil, fmt.Errorf("git add --all: %w", err)
	}

	staged, err := s.git.StagedDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("staged diff: %w", err)
	}

	unstaged, err := s.git.UnstagedDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("unstaged diff: %w", err)
	}

	if len(staged.Files) == 0 && len(unstaged.Files) == 0 {
		return nil, fmt.Errorf("no changes")
	}

	s.vlog(req, "staged files: %v", staged.Files)
	s.vlog(req, "unstaged files: %v", unstaged.Files)
	s.vlog(req, "diff lines: %d", staged.Lines+unstaged.Lines)

	// Auto-scope when no config or no scopes provided.
	if req.Config == nil || len(req.Config.Scopes) == 0 {
		if s.scopeSvc != nil {
			s.vlog(req, "auto-generating scopes...")
			scopes, err := s.scopeSvc.Generate(ctx, 200)
			if err != nil {
				s.vlog(req, "scope generation failed (continuing without scopes): %v", err)
			} else {
				_ = s.scopeSvc.MergeAndSave(ctx, ".git-agent/project.yml", scopes)
				req.Config = &project.Config{Scopes: scopes}
				s.vlog(req, "scopes: %v", scopes)
			}
		}
	}
	if req.Config == nil {
		req.Config = &project.Config{}
	}

	s.vlog(req, "planning commits...")
	plan, err := s.planner.Plan(ctx, commit.PlanRequest{
		StagedDiff:   staged,
		UnstagedDiff: unstaged,
		Intent:       req.Intent,
		Config:       req.Config,
	})
	if err != nil {
		return nil, fmt.Errorf("plan commits: %w", err)
	}

	remaining := make([]commit.CommitGroup, len(plan.Groups))
	copy(remaining, plan.Groups)
	s.vlog(req, "planned %d commit(s)", len(remaining))

	var committed []SingleCommitResult
	rePlanCount := 0

	for len(remaining) > 0 {
		group := remaining[0]
		remaining = remaining[1:]

		if err := s.git.UnstageAll(ctx); err != nil {
			return nil, fmt.Errorf("unstage all: %w", err)
		}
		if err := s.git.StageFiles(ctx, group.Files); err != nil {
			return nil, fmt.Errorf("stage files %v: %w", group.Files, err)
		}

		groupDiff, err := s.git.StagedDiff(ctx)
		if err != nil {
			return nil, fmt.Errorf("staged diff for group: %w", err)
		}

		var hookFeedback string
		var assembled string
		var msg *commit.CommitMessage
		hookPassed := false

		for attempt := 1; attempt <= maxHookRetries; attempt++ {
			s.vlog(req, "calling LLM... (attempt %d/%d)", attempt, maxHookRetries)

			msg, err = s.gen.Generate(ctx, commit.GenerateRequest{
				Diff:         groupDiff,
				Intent:       req.Intent,
				Config:       req.Config,
				HookFeedback: hookFeedback,
			})
			if err != nil {
				return nil, fmt.Errorf("generate commit message: %w", err)
			}
			s.vlog(req, "LLM response received")

			assembled = msg.Title
			if msg.Body != "" {
				assembled += "\n\n" + msg.Body
			}
			if len(req.CoAuthors) > 0 {
				var trailers []string
				for _, a := range req.CoAuthors {
					trailers = append(trailers, "Co-Authored-By: "+a)
				}
				assembled += "\n\n" + strings.Join(trailers, "\n")
			}

			if req.HookPath == "" {
				hookPassed = true
				break
			}

			hookResult, err := s.hookExec.Execute(ctx, req.HookPath, hook.HookInput{
				Diff:          groupDiff.Content,
				CommitMessage: assembled,
				Intent:        req.Intent,
				StagedFiles:   groupDiff.Files,
				Config:        *req.Config,
			})
			if err != nil {
				return nil, fmt.Errorf("hook execute: %w", err)
			}
			if hookResult.ExitCode == 0 {
				hookPassed = true
				break
			}

			s.out(req, "hook blocked (attempt %d/%d)", attempt, maxHookRetries)
			s.out(req, "commit message was:\n%s", assembled)
			if hookResult.Stderr != "" {
				s.out(req, "reason: %s", hookResult.Stderr)
			}
			hookFeedback = hookResult.Stderr
			if attempt < maxHookRetries {
				s.out(req, "retrying with hook feedback...")
			}
		}

		if !hookPassed {
			// Re-plan failed group + remaining files together (up to maxRePlans times).
			if rePlanCount >= maxRePlans {
				return nil, ErrHookBlocked
			}
			rePlanCount++
			s.out(req, "re-planning after hook failure (re-plan %d/%d)...", rePlanCount, maxRePlans)

			// Collect all files that still need to be committed.
			var allFiles []string
			allFiles = append(allFiles, group.Files...)
			for _, r := range remaining {
				allFiles = append(allFiles, r.Files...)
			}

			// Stage all remaining files to get their combined diff for re-planning.
			_ = s.git.UnstageAll(ctx)
			_ = s.git.StageFiles(ctx, allFiles)
			combinedDiff, _ := s.git.StagedDiff(ctx)
			_ = s.git.UnstageAll(ctx)

			newPlan, err := s.planner.Plan(ctx, commit.PlanRequest{
				StagedDiff: combinedDiff,
				Intent:     req.Intent,
				Config:     req.Config,
			})
			if err != nil {
				return nil, fmt.Errorf("re-plan commits: %w", err)
			}
			remaining = newPlan.Groups
			continue
		}

		result := SingleCommitResult{
			Outline: msg.Outline,
			Files:   group.Files,
			Title:   msg.Title,
		}
		if req.DryRun {
			committed = append(committed, result)
			continue
		}
		if err := s.git.Commit(ctx, assembled); err != nil {
			return nil, err
		}
		committed = append(committed, result)
	}

	return &CommitResult{Commits: committed, DryRun: req.DryRun}, nil
}
