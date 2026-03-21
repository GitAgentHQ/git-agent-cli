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
	FormatTrailers(ctx context.Context, message string, trailers []commit.Trailer) (string, error)
	RepoRoot(ctx context.Context) (string, error)
	LastCommitDiff(ctx context.Context) (*diff.StagedDiff, error)
	AmendCommit(ctx context.Context, message string) error
}

type CommitRequest struct {
	Intent            string
	Trailers          []commit.Trailer
	DryRun            bool
	NoStage           bool
	Amend             bool
	Config            *project.Config // nil = trigger auto-scope if scopeSvc provided; Config.HookType drives hook dispatch
	MaxLines          int
	Verbose           bool
	LogWriter         io.Writer // verbose-only output
	OutWriter         io.Writer // always-visible output (hook block context, retries)
	ProjectConfigPath string    // path to .git-agent/project.yml; empty = use default
}

type CommitService struct {
	gen       commit.CommitMessageGenerator
	planner   commit.CommitPlanner
	git       CommitGitClient
	hookExec  hook.HookExecutor
	scopeSvc  *ScopeService  // nil = no auto-scope
	filter    diff.DiffFilter    // nil = no filtering
	truncator diff.DiffTruncator // nil = no truncation
}

func NewCommitService(
	gen commit.CommitMessageGenerator,
	planner commit.CommitPlanner,
	git CommitGitClient,
	hookExec hook.HookExecutor,
	scopeSvc *ScopeService,
	filter diff.DiffFilter,
	truncator diff.DiffTruncator,
) *CommitService {
	return &CommitService{
		gen:       gen,
		planner:   planner,
		git:       git,
		hookExec:  hookExec,
		scopeSvc:  scopeSvc,
		filter:    filter,
		truncator: truncator,
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
const maxCommitGroups = 5

func (s *CommitService) Commit(ctx context.Context, req CommitRequest) (*CommitResult, error) {
	if req.Amend {
		return s.commitAmend(ctx, req)
	}

	var staged, unstaged *diff.StagedDiff

	if req.NoStage {
		fullStagedDiff, err := s.git.StagedDiff(ctx)
		if err != nil {
			return nil, fmt.Errorf("staged diff: %w", err)
		}
		if len(fullStagedDiff.Files) == 0 {
			return nil, fmt.Errorf("no staged changes (hint: stage files with git add, or remove --no-stage)")
		}
		staged = fullStagedDiff
		unstaged = &diff.StagedDiff{}
	} else {
		// Capture user's staging intent before AddAll erases the distinction.
		preStagedDiff, err := s.git.StagedDiff(ctx)
		if err != nil {
			return nil, fmt.Errorf("staged diff: %w", err)
		}
		userStagedFiles := make(map[string]bool, len(preStagedDiff.Files))
		for _, f := range preStagedDiff.Files {
			userStagedFiles[f] = true
		}

		if err := s.git.AddAll(ctx); err != nil {
			return nil, fmt.Errorf("git add --all: %w", err)
		}

		fullStagedDiff, err := s.git.StagedDiff(ctx)
		if err != nil {
			return nil, fmt.Errorf("staged diff: %w", err)
		}

		if len(fullStagedDiff.Files) == 0 {
			return nil, fmt.Errorf("no changes")
		}

		// Split into staged (user intent = group 0) vs unstaged (free to split).
		// If the user had nothing pre-staged, treat everything as unstaged so the
		// planner can create multiple atomic commits without the "group 0" constraint.
		if len(userStagedFiles) == 0 {
			staged = &diff.StagedDiff{}
			unstaged = fullStagedDiff
		} else {
			staged = preStagedDiff
			var newFiles []string
			for _, f := range fullStagedDiff.Files {
				if !userStagedFiles[f] {
					newFiles = append(newFiles, f)
				}
			}
			if len(newFiles) > 0 {
				unstaged = filterDiffByFiles(fullStagedDiff, newFiles)
			} else {
				unstaged = &diff.StagedDiff{}
			}
		}
	}

	var err error
	if s.filter != nil {
		if len(staged.Files) > 0 {
			staged, err = s.filter.Filter(ctx, staged)
			if err != nil {
				return nil, fmt.Errorf("filter staged diff: %w", err)
			}
		}
		if len(unstaged.Files) > 0 {
			unstaged, err = s.filter.Filter(ctx, unstaged)
			if err != nil {
				s.vlog(req, "filter unstaged diff (ignoring): %v", err)
				unstaged = &diff.StagedDiff{}
			}
		}
	}

	if s.truncator != nil && req.MaxLines > 0 {
		var truncated bool
		staged, truncated, err = s.truncator.Truncate(ctx, staged, req.MaxLines)
		if err != nil {
			return nil, fmt.Errorf("truncate staged diff: %w", err)
		}
		if truncated {
			s.vlog(req, "staged diff truncated to %d lines", req.MaxLines)
		}
		if len(unstaged.Files) > 0 {
			unstaged, truncated, err = s.truncator.Truncate(ctx, unstaged, req.MaxLines)
			if err != nil {
				return nil, fmt.Errorf("truncate unstaged diff: %w", err)
			}
			if truncated {
				s.vlog(req, "unstaged diff truncated to %d lines", req.MaxLines)
			}
		}
	}

	s.vlog(req, "staged files: %v", staged.Files)
	s.vlog(req, "unstaged files: %v", unstaged.Files)
	s.vlog(req, "diff lines: %d", staged.Lines+unstaged.Lines)

	// Build the allowed-files set for planner output validation.
	allowed := make(map[string]bool, len(staged.Files)+len(unstaged.Files))
	for _, f := range staged.Files {
		allowed[f] = true
	}
	for _, f := range unstaged.Files {
		allowed[f] = true
	}

	// Auto-scope when no config or no scopes provided.
	if req.Config == nil || len(req.Config.Scopes) == 0 {
		if s.scopeSvc != nil {
			s.vlog(req, "auto-generating scopes...")
			scopes, err := s.scopeSvc.Generate(ctx, 200)
			if err != nil {
				s.vlog(req, "scope generation failed (continuing without scopes): %v", err)
			} else {
				configPath := req.ProjectConfigPath
				if configPath == "" {
					configPath = ".git-agent/project.yml"
				}
				_ = s.scopeSvc.MergeAndSave(ctx, configPath, scopes)
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
	if n := filterPlanFiles(plan, allowed); n > 0 {
		s.vlog(req, "dropped %d hallucinated file(s) from plan", n)
	}
	if len(plan.Groups) > maxCommitGroups {
		s.vlog(req, "plan has %d groups — capping to %d", len(plan.Groups), maxCommitGroups)
		plan.Groups = plan.Groups[:maxCommitGroups]
	}

	// If any group has no scope and we can update scopes, do so and re-plan once.
	if s.scopeSvc != nil && len(req.Config.Scopes) > 0 && hasUnscopedGroups(plan) {
		s.vlog(req, "unscoped groups detected — refreshing project scopes...")
		newScopes, err := s.scopeSvc.Generate(ctx, 200)
		if err != nil {
			s.vlog(req, "scope refresh failed (continuing with current plan): %v", err)
		} else {
			configPath := req.ProjectConfigPath
			if configPath == "" {
				configPath = ".git-agent/project.yml"
			}
			_ = s.scopeSvc.MergeAndSave(ctx, configPath, newScopes)
			req.Config = &project.Config{Scopes: newScopes}
			s.vlog(req, "updated scopes: %v — re-planning...", newScopes)
			plan, err = s.planner.Plan(ctx, commit.PlanRequest{
				StagedDiff:   staged,
				UnstagedDiff: unstaged,
				Intent:       req.Intent,
				Config:       req.Config,
			})
			if err != nil {
				return nil, fmt.Errorf("re-plan after scope refresh: %w", err)
			}
			if n := filterPlanFiles(plan, allowed); n > 0 {
				s.vlog(req, "dropped %d hallucinated file(s) from re-plan", n)
			}
			if len(plan.Groups) > maxCommitGroups {
				s.vlog(req, "re-plan has %d groups — capping to %d", len(plan.Groups), maxCommitGroups)
				plan.Groups = plan.Groups[:maxCommitGroups]
			}
		}
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
		if len(groupDiff.Files) == 0 {
			s.vlog(req, "skipping group (no diff after staging): %v", group.Files)
			continue
		}

		var hookFeedback string
		var assembled string
		var msg *commit.CommitMessage
		hookPassed := false
		var previousMessage string

		for attempt := 1; attempt <= maxHookRetries; attempt++ {
			s.vlog(req, "calling LLM... (attempt %d/%d)", attempt, maxHookRetries)

			msg, err = s.gen.Generate(ctx, commit.GenerateRequest{
				Diff:            groupDiff,
				Intent:          req.Intent,
				Config:          req.Config,
				HookFeedback:    hookFeedback,
				PreviousMessage: previousMessage,
			})
			if err != nil {
				return nil, fmt.Errorf("generate commit message: %w", err)
			}
			s.vlog(req, "LLM response received")

			assembled = msg.Title
			if msg.Body != "" {
				assembled += "\n\n" + msg.Body
			}
			if len(req.Trailers) > 0 {
				var err2 error
				assembled, err2 = s.git.FormatTrailers(ctx, assembled, req.Trailers)
				if err2 != nil {
					return nil, fmt.Errorf("format trailers: %w", err2)
				}
			}

			if req.Config.HookType == "" || req.Config.HookType == "empty" {
				hookPassed = true
				break
			}

			hookResult, err := s.hookExec.Execute(ctx, req.Config.HookType, hook.HookInput{
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
			previousMessage = assembled
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
			if err := s.git.UnstageAll(ctx); err != nil {
				return nil, fmt.Errorf("re-plan unstage: %w", err)
			}
			if err := s.git.StageFiles(ctx, allFiles); err != nil {
				return nil, fmt.Errorf("re-plan stage files: %w", err)
			}
			combinedDiff, err := s.git.StagedDiff(ctx)
			if err != nil {
				return nil, fmt.Errorf("re-plan staged diff: %w", err)
			}
			if err := s.git.UnstageAll(ctx); err != nil {
				return nil, fmt.Errorf("re-plan unstage cleanup: %w", err)
			}

			newPlan, err := s.planner.Plan(ctx, commit.PlanRequest{
				StagedDiff: combinedDiff,
				Intent:     req.Intent,
				Config:     req.Config,
			})
			if err != nil {
				return nil, fmt.Errorf("re-plan commits: %w", err)
			}
			if n := filterPlanFiles(newPlan, allowed); n > 0 {
				s.vlog(req, "dropped %d hallucinated file(s) from hook re-plan", n)
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

	if len(committed) == 0 && !req.DryRun {
		return nil, fmt.Errorf("no changes committed (all planned groups were empty)")
	}

	return &CommitResult{Commits: committed, DryRun: req.DryRun}, nil
}

func (s *CommitService) commitAmend(ctx context.Context, req CommitRequest) (*CommitResult, error) {
	amendDiff, err := s.git.LastCommitDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("last commit diff: %w", err)
	}
	if len(amendDiff.Files) == 0 {
		return nil, fmt.Errorf("no previous commit to amend")
	}

	if s.filter != nil {
		amendDiff, err = s.filter.Filter(ctx, amendDiff)
		if err != nil {
			return nil, fmt.Errorf("filter diff: %w", err)
		}
	}

	if s.truncator != nil && req.MaxLines > 0 {
		var truncated bool
		amendDiff, truncated, err = s.truncator.Truncate(ctx, amendDiff, req.MaxLines)
		if err != nil {
			return nil, fmt.Errorf("truncate diff: %w", err)
		}
		if truncated {
			s.vlog(req, "diff truncated to %d lines", req.MaxLines)
		}
	}

	if req.Config == nil {
		req.Config = &project.Config{}
	}

	msg, err := s.gen.Generate(ctx, commit.GenerateRequest{
		Diff:   amendDiff,
		Intent: req.Intent,
		Config: req.Config,
	})
	if err != nil {
		return nil, fmt.Errorf("generate commit message: %w", err)
	}

	assembled := msg.Title
	if msg.Body != "" {
		assembled += "\n\n" + msg.Body
	}
	if len(req.Trailers) > 0 {
		assembled, err = s.git.FormatTrailers(ctx, assembled, req.Trailers)
		if err != nil {
			return nil, fmt.Errorf("format trailers: %w", err)
		}
	}

	result := SingleCommitResult{
		Outline: msg.Outline,
		Files:   amendDiff.Files,
		Title:   msg.Title,
	}
	if req.DryRun {
		return &CommitResult{Commits: []SingleCommitResult{result}, DryRun: true}, nil
	}
	if err := s.git.AmendCommit(ctx, assembled); err != nil {
		return nil, err
	}
	return &CommitResult{Commits: []SingleCommitResult{result}}, nil
}

// filterPlanFiles removes from each CommitGroup any file not in allowed, then
// drops groups with no remaining files. Returns the number of dropped files.
func filterPlanFiles(plan *commit.CommitPlan, allowed map[string]bool) int {
	filtered := 0
	kept := plan.Groups[:0]
	for i := range plan.Groups {
		var valid []string
		for _, f := range plan.Groups[i].Files {
			if allowed[f] {
				valid = append(valid, f)
			} else {
				filtered++
			}
		}
		plan.Groups[i].Files = valid
		if len(valid) > 0 {
			kept = append(kept, plan.Groups[i])
		}
	}
	plan.Groups = kept
	return filtered
}

// hasUnscopedGroups reports whether any commit group title lacks a scope,
// i.e. matches "type: description" instead of "type(scope): description".
func hasUnscopedGroups(plan *commit.CommitPlan) bool {
	for _, g := range plan.Groups {
		if !strings.Contains(g.Message.Title, "(") {
			return true
		}
	}
	return false
}

// filterDiffByFiles returns a new StagedDiff containing only the given files,
// extracting the relevant hunks from the diff content.
func filterDiffByFiles(d *diff.StagedDiff, files []string) *diff.StagedDiff {
	kept := make(map[string]bool, len(files))
	for _, f := range files {
		kept[f] = true
	}

	const prefix = "diff --git "
	parts := strings.Split(d.Content, prefix)

	var sb strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		firstLine := part
		if idx := strings.IndexByte(part, '\n'); idx >= 0 {
			firstLine = part[:idx]
		}
		bIdx := strings.LastIndex(firstLine, " b/")
		if bIdx < 0 {
			continue
		}
		if kept[firstLine[bIdx+3:]] {
			sb.WriteString(prefix)
			sb.WriteString(part)
		}
	}

	content := sb.String()
	return &diff.StagedDiff{
		Files:   files,
		Content: content,
		Lines:   strings.Count(content, "\n"),
	}
}
