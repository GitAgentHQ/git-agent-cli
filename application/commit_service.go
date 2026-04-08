package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/hook"
	"github.com/gitagenthq/git-agent/domain/project"
	pkgerrors "github.com/gitagenthq/git-agent/pkg/errors"
)

var ErrHookBlocked = errors.New("hook blocked commit")

// HookBlockedError is returned when a commit is blocked by the hook after all
// retries. LastMessage is the final assembled commit message that was rejected.
// Reason is the hook's last rejection output.
type HookBlockedError struct {
	LastMessage string
	Reason      string
}

func (e *HookBlockedError) Error() string { return ErrHookBlocked.Error() }
func (e *HookBlockedError) Is(target error) bool {
	return target == ErrHookBlocked
}

// SingleCommitResult holds the output of one committed group.
type SingleCommitResult struct {
	Title       string
	Explanation string
	GitOutput   string
	Files       []string
}

// CommitResult holds the output of a successful Commit call.
type CommitResult struct {
	Commits []SingleCommitResult
	DryRun  bool
}

type CommitGitClient interface {
	StagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	StagedDiffNumStat(ctx context.Context) (string, error)
	UnstagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	AllChangedFiles(ctx context.Context) ([]string, error)
	StageFiles(ctx context.Context, files []string) error
	UnstageAll(ctx context.Context) error
	Commit(ctx context.Context, message string) (string, error)
	FormatTrailers(ctx context.Context, message string, trailers []commit.Trailer) (string, error)
	RepoRoot(ctx context.Context) (string, error)
	LastCommitDiff(ctx context.Context) (*diff.StagedDiff, error)
	AmendCommit(ctx context.Context, message string) (string, error)
}

type CommitRequest struct {
	Intent            string
	Trailers          []commit.Trailer
	DryRun            bool
	NoStage           bool
	Amend             bool
	Config            *project.Config // nil = trigger auto-scope if scopeSvc provided; Config.Hooks drives hook dispatch
	MaxLines          int
	MaxBytes          int // 0 = DefaultMaxDiffBytes
	Verbose           bool
	LogWriter         io.Writer // verbose-only output
	OutWriter         io.Writer // always-visible output (hook block context, retries)
	ProjectConfigPath string    // path to .git-agent/project.yml; empty = use default
}

type CommitService struct {
	gen              commit.CommitMessageGenerator
	planner          commit.CommitPlanner
	git              CommitGitClient
	hookExec         hook.HookExecutor
	scopeSvc         *ScopeService           // nil = no auto-scope
	filter           diff.DiffFilter         // nil = no filtering
	truncator        diff.DiffTruncator      // nil = no truncation
	heuristicPlanner commit.HeuristicPlanner // nil = no REQ-008 fallback
	coChange         CoChangeProvider        // nil = skip co-change (graceful)
	actionLinker     ActionLinker            // nil = skip action-to-commit linking (graceful)
}

func NewCommitService(
	gen commit.CommitMessageGenerator,
	planner commit.CommitPlanner,
	git CommitGitClient,
	hookExec hook.HookExecutor,
	scopeSvc *ScopeService,
	filter diff.DiffFilter,
	truncator diff.DiffTruncator,
	heuristicPlanner commit.HeuristicPlanner,
	coChange ...CoChangeProvider,
) *CommitService {
	var cp CoChangeProvider
	if len(coChange) > 0 {
		cp = coChange[0]
	}
	return &CommitService{
		gen:              gen,
		planner:          planner,
		git:              git,
		hookExec:         hookExec,
		scopeSvc:         scopeSvc,
		filter:           filter,
		truncator:        truncator,
		heuristicPlanner: heuristicPlanner,
		coChange:         cp,
	}
}

// HeuristicPlanner reports the fallback planner this service uses when the
// primary LLM planner exhausts its token budget. Returns nil when REQ-008
// fallback is disabled. Exposed so cmd-layer wiring tests can confirm
// plan_fallback=heuristic actually constructs the fallback collaborator.
func (s *CommitService) HeuristicPlanner() commit.HeuristicPlanner {
	return s.heuristicPlanner
}

// runPlan invokes the configured planner and, when the LLM planner cannot
// produce a plan (budget exhausted OR per-attempt timeout), falls back to the
// heuristic planner unless the project has explicitly opted out via
// plan_fallback=none. Other plan errors propagate unchanged.
//
// Default (PlanFallback unset, empty string, or "auto"): fallback enabled.
// Default exists because the LLM planner is the dominant failure mode for
// large diffs and agent-driven workflows; surfacing a hard error there
// wastes the agent's time on a path the heuristic bucketer can handle
// deterministically.
func (s *CommitService) runPlan(ctx context.Context, req CommitRequest, planReq commit.PlanRequest) (*commit.CommitPlan, error) {
	plan, err := s.planner.Plan(ctx, planReq)
	if err == nil {
		return plan, nil
	}
	if !isPlannerFallbackError(err) {
		return nil, err
	}
	if s.heuristicPlanner == nil {
		return nil, err
	}
	if req.Config != nil && req.Config.PlanFallback == project.PlanFallbackNone {
		return nil, err
	}
	s.out(req, "Warning: LLM planner unavailable (%s), falling back to directory bucketer", plannerFallbackReason(err))
	return s.heuristicPlanner.Plan(ctx, planReq)
}

// isPlannerFallbackError reports whether err is one of the LLM-planner
// failures the heuristic bucketer can substitute for.
func isPlannerFallbackError(err error) bool {
	return errors.Is(err, commit.ErrPlannerBudgetExhausted) ||
		errors.Is(err, commit.ErrPlannerTimedOut)
}

// plannerFallbackReason renders a short tag identifying which planner failure
// triggered the fallback, for the always-on phase line.
func plannerFallbackReason(err error) string {
	switch {
	case errors.Is(err, commit.ErrPlannerTimedOut):
		return "timed out"
	case errors.Is(err, commit.ErrPlannerBudgetExhausted):
		return "budget exhausted"
	default:
		return "error"
	}
}

// SetActionLinker sets an optional action-to-commit linker.
func (s *CommitService) SetActionLinker(linker ActionLinker) {
	s.actionLinker = linker
}

// SetCoChangeProvider sets an optional co-change hint provider.
func (s *CommitService) SetCoChangeProvider(provider CoChangeProvider) {
	s.coChange = provider
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

// DefaultMaxDiffBytes caps the byte size of the diff sent to the LLM when the
// caller sets no explicit limit. git-agent-proxy rejects request bodies over
// 512 KiB; 384 KiB of raw diff keeps the JSON-escaped body (plus system prompt
// and envelope) under that gate, and comfortably under the AI Gateway's larger
// limit too. Override with --max-diff-bytes / max_diff_bytes for endpoints that
// allow more. Unlike the line cap, this guard is always applied — large
// vendored or minified files have few lines but many bytes, so the line cap
// alone cannot bound the request body.
const DefaultMaxDiffBytes = 384 << 10

// effectiveMaxBytes resolves the byte cap, falling back to the built-in default
// when the caller passes 0 or a negative value. The request body is always
// bounded — there is no "disable" path, because every supported endpoint
// (proxy, AI Gateway, OpenAI) imposes a body-size limit smaller than typical
// vendored diffs. To raise the cap, pass a positive value via --max-diff-bytes
// or the max_diff_bytes config key.
func effectiveMaxBytes(maxBytes int) int {
	if maxBytes <= 0 {
		return DefaultMaxDiffBytes
	}
	return maxBytes
}

// truncationLimitDesc renders the active caps for verbose logging, omitting the
// line component when no line cap is in effect (so it never reads "max 0 lines").
func truncationLimitDesc(maxLines, maxBytes int) string {
	if maxLines > 0 {
		return fmt.Sprintf("max %d lines / %d bytes", maxLines, maxBytes)
	}
	return fmt.Sprintf("max %d bytes", maxBytes)
}

func (s *CommitService) Commit(ctx context.Context, req CommitRequest) (_ *CommitResult, retErr error) {
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
		// Capture which files the user pre-staged so they anchor group 0.
		// Only the file list is preserved — partial-hunk staging is collapsed
		// when the per-group loop re-stages the working tree.
		preStagedDiff, err := s.git.StagedDiff(ctx)
		if err != nil {
			return nil, fmt.Errorf("staged diff: %w", err)
		}
		userStagedFiles := make(map[string]bool, len(preStagedDiff.Files))
		for _, f := range preStagedDiff.Files {
			userStagedFiles[f] = true
		}

		// Get all changed files WITHOUT modifying the index.
		allFiles, err := s.git.AllChangedFiles(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing changed files: %w", err)
		}
		if len(allFiles) == 0 {
			return nil, fmt.Errorf("no changes")
		}

		// Split into staged (user intent = group 0) vs unstaged (free to split).
		// If the user had nothing pre-staged, treat everything as unstaged so the
		// planner can create multiple atomic commits without the "group 0" constraint.
		if len(userStagedFiles) == 0 {
			staged = &diff.StagedDiff{}
			unstaged = &diff.StagedDiff{Files: allFiles}
		} else {
			var userFiles []string
			var autoFiles []string
			for _, f := range allFiles {
				if userStagedFiles[f] {
					userFiles = append(userFiles, f)
				} else {
					autoFiles = append(autoFiles, f)
				}
			}
			staged = &diff.StagedDiff{Files: userFiles}
			if len(autoFiles) > 0 {
				unstaged = &diff.StagedDiff{Files: autoFiles}
			} else {
				unstaged = &diff.StagedDiff{}
			}
		}
	}

	// staged and unstaged carry only file lists at this point — the planner
	// reads .Files only, and per-group filter/truncate runs on groupDiff
	// inside the commit loop where actual diff content is available.

	s.vlog(req, "staged files: %v", staged.Files)
	s.vlog(req, "unstaged files: %v", unstaged.Files)

	// Build the allowed-files set for planner output validation.
	allowed := make(map[string]bool, len(staged.Files)+len(unstaged.Files))
	for _, f := range staged.Files {
		allowed[f] = true
	}
	for _, f := range unstaged.Files {
		allowed[f] = true
	}

	// Ensure req.Config is non-nil up front so auto-scope can mutate Scopes
	// in place and the per-group loop can read Hooks / co-author policy.
	if req.Config == nil {
		req.Config = &project.Config{}
	}

	// Auto-scope when no scopes provided — assign Scopes in place so Hooks,
	// MaxDiffLines, MaxDiffBytes, and co-author policy survive the refresh.
	if len(req.Config.Scopes) == 0 {
		if s.scopeSvc != nil {
			s.out(req, "Generating scopes...")
			scopes, err := s.scopeSvc.Generate(ctx, 200, nil)
			if err != nil {
				s.out(req, "Warning: failed to generate scopes, continuing without scopes (%v)", err)
			} else {
				configPath := req.ProjectConfigPath
				if configPath == "" {
					configPath = ".git-agent/project.yml"
				}
				if err := s.scopeSvc.MergeAndSave(ctx, configPath, scopes); err != nil {
					s.vlog(req, "save scopes (non-fatal): %v", err)
				}
				req.Config.Scopes = scopes
				s.vlog(req, "scopes: %v", req.Config.ScopeNames())
			}
		}
	}

	var (
		plan *commit.CommitPlan
		err  error
	)

	allFiles := make([]string, 0, len(allowed))
	for f := range allowed {
		allFiles = append(allFiles, f)
	}

	// Inject co-change hints if available.
	var coChangeHints []commit.CoChangeHint
	if s.coChange != nil {
		hints, cerr := s.coChange.GetHintsForFiles(ctx, allFiles)
		if cerr != nil {
			s.vlog(req, "co-change lookup failed: %v", cerr)
		} else if len(hints) > 0 {
			coChangeHints = hints
			s.vlog(req, "found %d co-change hints for planning", len(hints))
		}
	}

	if len(allFiles) == 1 {
		s.vlog(req, "single file — skipping planning phase")
		plan = &commit.CommitPlan{Groups: []commit.CommitGroup{{Files: allFiles}}}
	} else {
		s.out(req, "Planning commits...")
		plan, err = s.runPlan(ctx, req, commit.PlanRequest{
			StagedDiff:    staged,
			UnstagedDiff:  unstaged,
			Intent:        req.Intent,
			Config:        req.Config,
			CoChangeHints: coChangeHints,
		})
		if err != nil {
			return nil, fmt.Errorf("plan commits: %w", err)
		}
		if n := filterPlanFiles(plan, allowed); n > 0 {
			s.vlog(req, "dropped %d hallucinated file(s) from plan", n)
		}
		if len(plan.Groups) == 0 {
			return nil, fmt.Errorf("plan produced no valid commit groups (all files were filtered out)")
		}
		if len(plan.Groups) > maxCommitGroups {
			s.out(req, "Warning: commit plan exceeds group limit (%d > %d), capping", len(plan.Groups), maxCommitGroups)
			plan.Groups = plan.Groups[:maxCommitGroups]
		}
		appendPassthroughFiles(plan, allowed)

		// If any group has no scope and we can update scopes, do so and re-plan once.
		if s.scopeSvc != nil && len(req.Config.Scopes) > 0 && hasUnscopedGroups(plan) {
			s.out(req, "Refreshing scopes...")
			newScopes, err := s.scopeSvc.Generate(ctx, 200, req.Config.Scopes)
			if err != nil {
				s.vlog(req, "scope refresh failed (continuing with current plan): %v", err)
			} else {
				configPath := req.ProjectConfigPath
				if configPath == "" {
					configPath = ".git-agent/project.yml"
				}
				if err := s.scopeSvc.MergeAndSave(ctx, configPath, newScopes); err != nil {
					s.vlog(req, "save scopes (non-fatal): %v", err)
				}
				req.Config.Scopes = newScopes
				s.out(req, "Scopes updated: %v, re-planning...", req.Config.ScopeNames())
				plan, err = s.runPlan(ctx, req, commit.PlanRequest{
					StagedDiff:    staged,
					UnstagedDiff:  unstaged,
					Intent:        req.Intent,
					Config:        req.Config,
					CoChangeHints: coChangeHints,
				})
				if err != nil {
					return nil, fmt.Errorf("re-plan after scope refresh: %w", err)
				}
				if n := filterPlanFiles(plan, allowed); n > 0 {
					s.vlog(req, "dropped %d hallucinated file(s) from re-plan", n)
				}
				if len(plan.Groups) == 0 {
					return nil, fmt.Errorf("re-plan produced no valid commit groups (all files were filtered out)")
				}
				if len(plan.Groups) > maxCommitGroups {
					s.vlog(req, "re-plan has %d groups — capping to %d", len(plan.Groups), maxCommitGroups)
					plan.Groups = plan.Groups[:maxCommitGroups]
				}
				appendPassthroughFiles(plan, allowed)
			}
		}
	}

	remaining := make([]commit.CommitGroup, len(plan.Groups))
	copy(remaining, plan.Groups)
	totalGroups := len(plan.Groups)
	commitWord := "commits"
	if totalGroups == 1 {
		commitWord = "commit"
	}
	s.out(req, "Planning commits: done (%d %s).", totalGroups, commitWord)

	var committed []SingleCommitResult
	committedFiles := make(map[string]bool)
	rePlanCount := 0
	var inheritedFeedback string // hook feedback carried into first attempt after re-plan

	// On error, best-effort re-stage uncommitted files so the index is not
	// left in a partially-unstaged state from the UnstageAll/StageFiles loop.
	defer func() {
		if retErr == nil {
			return
		}
		var toRestore []string
		for f := range allowed {
			if !committedFiles[f] {
				toRestore = append(toRestore, f)
			}
		}
		if len(toRestore) > 0 {
			sort.Strings(toRestore)
			_ = s.git.StageFiles(context.Background(), toRestore)
		}
	}()

	groupIdx := 0
	for len(remaining) > 0 {
		group := remaining[0]
		remaining = remaining[1:]
		groupIdx++

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

		// Filter groupDiff content for LLM input only — keeps lock files and binaries
		// out of the prompt while the hook still receives the full raw diff.
		genDiff := groupDiff
		if s.filter != nil {
			if fd, ferr := s.filter.Filter(ctx, groupDiff); ferr == nil {
				genDiff = fd
			}
		}
		if s.truncator != nil {
			maxBytes := effectiveMaxBytes(req.MaxBytes)
			truncated, didTruncate, terr := s.truncator.Truncate(ctx, genDiff, req.MaxLines, maxBytes)
			if terr != nil {
				return nil, fmt.Errorf("truncate group diff: %w", terr)
			}
			if didTruncate {
				s.vlog(req, "group diff truncated (%s)", truncationLimitDesc(req.MaxLines, maxBytes))
			}
			genDiff = truncated

			// REQ-007 — saturation fallback. When a single-file diff saturates the
			// byte cap, head-truncation produces a useless prompt (typically the
			// file header repeated). Replace it with a compact DIFF-SYNOPSIS so
			// the LLM still receives a meaningful commit context. The raw diff on
			// groupDiff.Content is untouched so the hook continues to see the
			// full payload.
			if didTruncate && len(genDiff.Files) == 1 && len(genDiff.Content) == maxBytes {
				stat, statErr := s.git.StagedDiffNumStat(ctx)
				if statErr == nil {
					synopsis := buildSynopsis(genDiff.Files[0], stat, len(groupDiff.Content), maxBytes)
					genDiff = &diff.StagedDiff{
						Files:   genDiff.Files,
						Content: synopsis,
						Lines:   strings.Count(synopsis, "\n"),
					}
					s.out(req, "Warning: commit %d/%d: falling back to diff synopsis for %s", groupIdx, totalGroups, genDiff.Files[0])
				} else {
					s.vlog(req, "stat fallback failed (continuing with truncated diff): %v", statErr)
					s.out(req, "Warning: commit %d/%d: diff exceeds limit, truncating to %d bytes", groupIdx, totalGroups, maxBytes)
				}
			} else if didTruncate {
				s.out(req, "Warning: commit %d/%d: diff exceeds limit, truncating to %d bytes", groupIdx, totalGroups, maxBytes)
			}
		}

		// Seed hook feedback from previous re-plan failure so the first Generate
		// call already knows the validation constraints that caused the re-plan.
		// previousMessage is intentionally empty: we want the full diff path (not
		// the retry-only path) so the LLM sees the diff AND the constraint hints.
		hookFeedback := inheritedFeedback
		inheritedFeedback = ""
		var assembled string
		var msg *commit.CommitMessage
		hookPassed := false
		var previousMessage string
		var preTrailer string // assembled before trailers — used for HookBlockedError

		for attempt := 1; attempt <= maxHookRetries; attempt++ {
			if attempt == 1 {
				s.out(req, "Drafting message: %d/%d...", groupIdx, totalGroups)
			}

			msg, err = s.gen.Generate(ctx, commit.GenerateRequest{
				Diff:            genDiff,
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
			if body := msg.Body(); body != "" {
				assembled += "\n\n" + body
			}
			preTrailer = assembled

			if len(req.Trailers) > 0 {
				var err2 error
				assembled, err2 = s.git.FormatTrailers(ctx, assembled, req.Trailers)
				if err2 != nil {
					return nil, fmt.Errorf("format trailers: %w", err2)
				}
			}

			if len(req.Config.Hooks) == 0 && !req.Config.RequireModelCoAuthor {
				hookPassed = true
				break
			}

			hookResult, err := s.hookExec.Execute(ctx, req.Config.Hooks, hook.HookInput{
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

			if attempt < maxHookRetries {
				s.out(req, "Warning: hook rejected message, retrying... (attempt %d/%d)", attempt+1, maxHookRetries)
			}
			hookFeedback = hookResult.Stderr
			previousMessage = preTrailer
		}

		if !hookPassed {
			// Re-plan failed group + remaining files together (up to maxRePlans times).
			if rePlanCount >= maxRePlans {
				return nil, &HookBlockedError{LastMessage: preTrailer, Reason: hookFeedback}
			}
			// Carry the last hook feedback so the first Generate call of each
			// re-planned group already knows why previous attempts were rejected.
			inheritedFeedback = hookFeedback
			rePlanCount++

			// Collect all files that still need to be committed.
			var allFiles []string
			allFiles = append(allFiles, group.Files...)
			for _, r := range remaining {
				allFiles = append(allFiles, r.Files...)
			}

			newPlan, err := s.planner.Plan(ctx, commit.PlanRequest{
				StagedDiff: &diff.StagedDiff{Files: allFiles},
				Intent:     req.Intent,
				Config:     req.Config,
			})
			if err != nil {
				return nil, fmt.Errorf("re-plan commits: %w", err)
			}
			rePlanAllowed := make(map[string]bool, len(allowed))
			for f := range allowed {
				if !committedFiles[f] {
					rePlanAllowed[f] = true
				}
			}
			if n := filterPlanFiles(newPlan, rePlanAllowed); n > 0 {
				s.vlog(req, "dropped %d hallucinated file(s) from hook re-plan", n)
			}
			appendPassthroughFiles(newPlan, rePlanAllowed)
			remaining = newPlan.Groups
			continue
		}

		result := SingleCommitResult{
			Title:       msg.Title,
			Explanation: msg.Explanation,
			Files:       group.Files,
		}
		if req.DryRun {
			committed = append(committed, result)
			for _, f := range group.Files {
				committedFiles[f] = true
			}
			continue
		}
		gitOut, err := s.git.Commit(ctx, assembled)
		if err != nil {
			if errors.Is(err, pkgerrors.ErrNothingToCommit) {
				s.vlog(req, "skipping group (nothing to commit at commit time): %v", group.Files)
				continue
			}
			return nil, err
		}
		result.GitOutput = gitOut
		committed = append(committed, result)

		// Link unlinked actions to this commit (graceful — never fails the commit)
		if s.actionLinker != nil {
			if linkErr := s.actionLinker.LinkActionsToCommit(ctx, gitOut, group.Files); linkErr != nil {
				s.vlog(req, "action-to-commit linking failed: %v", linkErr)
			}
		}
		for _, f := range group.Files {
			committedFiles[f] = true
		}
	}

	if len(committed) == 0 && !req.DryRun {
		return nil, fmt.Errorf("no changes committed: all %d planned group(s) skipped (no diff after staging)", len(plan.Groups))
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

	if s.truncator != nil {
		maxBytes := effectiveMaxBytes(req.MaxBytes)
		var truncated bool
		amendDiff, truncated, err = s.truncator.Truncate(ctx, amendDiff, req.MaxLines, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("truncate diff: %w", err)
		}
		if truncated {
			s.out(req, "Warning: diff truncated (%s)", truncationLimitDesc(req.MaxLines, maxBytes))
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
	if body := msg.Body(); body != "" {
		assembled += "\n\n" + body
	}
	if len(req.Trailers) > 0 {
		assembled, err = s.git.FormatTrailers(ctx, assembled, req.Trailers)
		if err != nil {
			return nil, fmt.Errorf("format trailers: %w", err)
		}
	}

	result := SingleCommitResult{
		Title:       msg.Title,
		Explanation: msg.Explanation,
		Files:       amendDiff.Files,
	}
	if req.DryRun {
		return &CommitResult{Commits: []SingleCommitResult{result}, DryRun: true}, nil
	}
	gitOut, err := s.git.AmendCommit(ctx, assembled)
	if err != nil {
		return nil, err
	}
	result.GitOutput = gitOut
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

// appendPassthroughFiles adds any file present in allowed but absent from every
// plan group to the first group. This covers content-filtered files (lock files,
// binaries) that must still be staged and committed but whose diff was not shown
// to the planner.
func appendPassthroughFiles(plan *commit.CommitPlan, allowed map[string]bool) {
	if len(plan.Groups) == 0 {
		return
	}
	inPlan := make(map[string]bool)
	for _, g := range plan.Groups {
		for _, f := range g.Files {
			inPlan[f] = true
		}
	}
	var passthrough []string
	for f := range allowed {
		if !inPlan[f] {
			passthrough = append(passthrough, f)
		}
	}
	if len(passthrough) == 0 {
		return
	}
	sort.Strings(passthrough)
	plan.Groups[0].Files = append(plan.Groups[0].Files, passthrough...)
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

// buildSynopsis renders the DIFF-SYNOPSIS block used when a single-file diff
// saturates the byte cap. The stat-line is expected in the shape
// "<adds>\t<dels>\t<path>" produced by `git diff --staged --numstat`; when the
// add/delete counters cannot be parsed they default to zero so the prompt
// stays well-formed.
func buildSynopsis(file, statLine string, actualBytes, capBytes int) string {
	adds, dels := parseNumStat(statLine, file)
	return fmt.Sprintf(
		"DIFF-SYNOPSIS\nfile: %s\nchanges: +%d / -%d (stat)\nnote: full diff elided (%d bytes exceeded %d-byte cap)\n",
		file, adds, dels, actualBytes, capBytes,
	)
}

// parseNumStat extracts the `+adds` / `-dels` counts from a single
// `git diff --staged --numstat` line whose path matches file. Lines have the
// shape "<adds>\t<dels>\t<path>". Binary files report "-" for counts.
func parseNumStat(numstat, file string) (adds, dels int) {
	for _, raw := range strings.Split(numstat, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == file {
			fmt.Sscanf(fields[0], "%d", &adds)
			fmt.Sscanf(fields[1], "%d", &dels)
			return adds, dels
		}
	}
	return 0, 0
}
