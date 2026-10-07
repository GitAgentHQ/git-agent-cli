package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/decision"
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

// SingleCommitResult holds the output of one committed group. The JSON tags are
// part of the agent-facing `commit -o json` contract; GitOutput and Explanation
// drive only the human text rendering and are excluded from JSON.
type SingleCommitResult struct {
	Title       string   `json:"title"`
	Message     string   `json:"message"` // full message (title + body), without trailers
	Explanation string   `json:"-"`       // closing paragraph; text output only
	GitOutput   string   `json:"-"`       // raw `git commit` stdout; text output only
	Files       []string `json:"files"`
	SHA         string   `json:"sha,omitempty"`          // commit hash; empty on dry-run
	HookOutcome string   `json:"hook_outcome,omitempty"` // passed | skipped
}

// CommitResult holds the output of a successful Commit call.
type CommitResult struct {
	Commits []SingleCommitResult `json:"commits"`
	DryRun  bool                 `json:"dry_run"`
}

type CommitGitClient interface {
	StagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	StagedDiffNumStat(ctx context.Context) (string, error)
	UnstagedDiff(ctx context.Context) (*diff.StagedDiff, error)
	AllChangedFiles(ctx context.Context) ([]string, error)
	DetectRenames(ctx context.Context) ([]diff.Rename, error)
	StageFiles(ctx context.Context, files []string) error
	UnstageAll(ctx context.Context) error
	Commit(ctx context.Context, message string) (string, error)
	CommitHash(ctx context.Context) (string, error)
	FormatTrailers(ctx context.Context, message string, trailers []commit.Trailer) (string, error)
	RepoRoot(ctx context.Context) (string, error)
	LastCommitDiff(ctx context.Context) (*diff.StagedDiff, error)
	AmendCommit(ctx context.Context, message string) (string, error)
}

type CommitRequest struct {
	Intent       string
	Trailers     []commit.Trailer
	DryRun       bool
	NoStage      bool
	Amend        bool
	Config       *project.Config // nil = trigger auto-scope if scopeSvc provided; Config.Hooks drives hook dispatch
	MaxLines     int
	MaxBytes     int // 0 = DefaultMaxDiffBytes
	MaxPlanFiles int // 0 = commit.DefaultMaxPlanFiles; caps planner prompt file-list size
	Verbose      bool
	LogWriter    io.Writer // verbose-only output
	OutWriter    io.Writer // always-visible output (hook block context, retries)
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
	// decider decides how files group into commits. A nil decider leaves the
	// grouping to the planner.
	decider commit.GroupDecider
	// judge decides the type and scope prefix of each message. A nil judge
	// leaves the whole title to the model.
	judge commit.TypeScopeJudge
	// router decides which lever a rejected commit needs. A nil router keeps
	// the fixed retry policy.
	router    commit.FailureRouter
	decisions *Decisions
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
) *CommitService {
	return &CommitService{
		gen:              gen,
		planner:          planner,
		git:              git,
		hookExec:         hookExec,
		scopeSvc:         scopeSvc,
		filter:           filter,
		truncator:        truncator,
		heuristicPlanner: heuristicPlanner,
	}
}

// WithJudgments attaches the System One seams: the grouping decider, the
// type and scope judge, and the failure router. Any of them may be nil, which
// leaves that decision to the existing path.
func (s *CommitService) WithJudgments(decider commit.GroupDecider, judge commit.TypeScopeJudge, router commit.FailureRouter) *CommitService {
	s.decider, s.judge, s.router = decider, judge, router
	return s
}

// WithDecisions attaches the System One policy for one run.
func (s *CommitService) WithDecisions(d *Decisions) *CommitService {
	s.decisions = d
	return s
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
		if plan == nil {
			return nil, errors.New("planner returned nil commit plan")
		}
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
	plan, err = s.heuristicPlanner.Plan(ctx, planReq)
	if err == nil && plan == nil {
		return nil, errors.New("heuristic planner returned nil commit plan")
	}
	return plan, err
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
// caller sets no explicit limit. 384 KiB of raw diff stays under the free
// gateway's 512 KiB body gate after JSON escaping (Go's json.Marshal HTML-
// escapes <, >, & and doubles quotes/backslashes/newlines), with headroom for
// the system prompt and envelope. A quote/backslash-heavy diff near this cap
// can still escape past 512 KiB — that's why the Worker also rejects oversized
// bodies. Override with --max-diff-bytes / max_diff_bytes for endpoints that
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

func (s *CommitService) Commit(ctx context.Context, req CommitRequest) (*CommitResult, error) {
	if req.Amend {
		return s.commitAmend(ctx, req)
	}
	if req.Config == nil {
		req.Config = &project.Config{}
	}
	return s.commitWorkflow(ctx, req)
}

func (s *CommitService) commitWorkflow(ctx context.Context, req CommitRequest) (*CommitResult, error) {
	staged, unstaged, err := s.collectChanges(ctx, req)
	if err != nil {
		return nil, err
	}
	allowed := allowedFiles(staged, unstaged)
	s.vlog(req, "staged files: %v", staged.Files)
	s.vlog(req, "unstaged files: %v", unstaged.Files)

	if err := s.ensureScopes(ctx, req); err != nil {
		return nil, err
	}
	renamed, err := s.detectRenames(ctx, req)
	if err != nil {
		return nil, err
	}
	plan, err := s.buildPlan(ctx, req, staged, unstaged, allowed, renamed)
	if err != nil {
		return nil, err
	}
	return s.commitGroups(ctx, req, plan, allowed, renamed)
}

func (s *CommitService) collectChanges(ctx context.Context, req CommitRequest) (*diff.StagedDiff, *diff.StagedDiff, error) {
	if req.NoStage {
		staged, err := s.git.StagedDiff(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("staged diff: %w", err)
		}
		if len(staged.Files) == 0 {
			return nil, nil, fmt.Errorf("no staged changes (hint: stage files with git add, or remove --no-stage)")
		}
		return staged, &diff.StagedDiff{}, nil
	}
	preStaged, err := s.git.StagedDiff(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("staged diff: %w", err)
	}
	stagedSet := make(map[string]bool, len(preStaged.Files))
	for _, file := range preStaged.Files {
		stagedSet[file] = true
	}
	allFiles, err := s.git.AllChangedFiles(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing changed files: %w", err)
	}
	if len(allFiles) == 0 {
		return nil, nil, fmt.Errorf("no changes")
	}
	if len(stagedSet) == 0 {
		return &diff.StagedDiff{}, &diff.StagedDiff{Files: allFiles}, nil
	}
	var stagedFiles, unstagedFiles []string
	for _, file := range allFiles {
		if stagedSet[file] {
			stagedFiles = append(stagedFiles, file)
		} else {
			unstagedFiles = append(unstagedFiles, file)
		}
	}
	return &diff.StagedDiff{Files: stagedFiles}, &diff.StagedDiff{Files: unstagedFiles}, nil
}

func allowedFiles(staged, unstaged *diff.StagedDiff) map[string]bool {
	allowed := make(map[string]bool, len(staged.Files)+len(unstaged.Files))
	for _, file := range staged.Files {
		allowed[file] = true
	}
	for _, file := range unstaged.Files {
		allowed[file] = true
	}
	return allowed
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *CommitService) ensureScopes(ctx context.Context, req CommitRequest) error {
	if len(req.Config.Scopes) > 0 || s.scopeSvc == nil {
		return nil
	}
	s.out(req, "Generating scopes...")
	scopes, err := s.scopeSvc.Generate(ctx, 200, nil)
	if err != nil {
		s.out(req, "Warning: failed to generate scopes, continuing without scopes (%v)", err)
		return nil
	}
	req.Config.Scopes = scopes
	s.vlog(req, "scopes (in-memory): %v", req.Config.ScopeNames())
	return nil
}

func (s *CommitService) detectRenames(ctx context.Context, req CommitRequest) ([]diff.Rename, error) {
	renamed, err := s.git.DetectRenames(ctx)
	if err != nil {
		s.vlog(req, "rename detection failed (non-fatal): %v", err)
		return nil, nil
	}
	if len(renamed) > 0 {
		s.vlog(req, "detected %d rename(s)", len(renamed))
	}
	return renamed, nil
}

func (s *CommitService) buildPlan(ctx context.Context, req CommitRequest, staged, unstaged *diff.StagedDiff, allowed map[string]bool, renamed []diff.Rename) (*commit.CommitPlan, error) {
	files := sortedKeys(allowed)
	if len(files) == 1 {
		s.vlog(req, "single file — skipping planning phase")
		return &commit.CommitPlan{Groups: []commit.CommitGroup{{Files: files}}}, nil
	}
	s.out(req, "Planning commits...")
	plan, err := s.plan(ctx, req, staged, unstaged, allowed, renamed)
	if err != nil {
		return nil, fmt.Errorf("plan commits: %w", err)
	}
	s.normalizePlan(plan, allowed, renamed, req)
	if s.scopeSvc != nil && len(req.Config.Scopes) > 0 && hasUnscopedGroups(plan) {
		if err := s.refreshScopesAndPlan(ctx, req, staged, unstaged, allowed, renamed, plan); err != nil {
			return nil, err
		}
	}
	if len(plan.Groups) == 0 {
		return nil, fmt.Errorf("plan produced no valid commit groups (all files were filtered out)")
	}
	return plan, nil
}

// routeHookFailure asks the router which lever the rejection needs. Every other
// case, including a router failure, keeps the fixed policy of re-planning.
//
// The reason the router returns replan is handled by the caller falling through
// to the existing re-plan, so this function only needs to distinguish the two
// outcomes that change the loop.
func (s *CommitService) routeHookFailure(ctx context.Context, req CommitRequest, group commit.CommitGroup, remaining []commit.CommitGroup, attempt *generatedGroupMessage) commit.FailureAction {
	session := s.decisions.Begin()
	if s.router == nil || !session.Take(SeamHookRoute) {
		return ""
	}

	var remainingFiles []string
	for _, pending := range remaining {
		remainingFiles = append(remainingFiles, pending.Files...)
	}

	started := time.Now()
	route, err := s.router.RouteFailure(ctx, commit.FailureRouteRequest{
		HookReason:     attempt.hookFeedback,
		Title:          attempt.preTrailer,
		Files:          group.Files,
		RemainingFiles: remainingFiles,
	})
	if err != nil {
		session.Report(started, decision.Observation{Seam: SeamHookRoute, Err: err.Error()})
		return ""
	}

	floor, ok := hookRouteFloorOf(s.router)
	if ok && !session.AdoptAt(SeamHookRoute, route.Confidence, floor) {
		session.Report(started, decision.Observation{
			Seam: SeamHookRoute, Decision: string(route.Action), Confidence: route.Confidence,
		})
		return ""
	}
	session.Report(started, decision.Observation{
		Seam: SeamHookRoute, Decision: string(route.Action), Confidence: route.Confidence, Adopted: true,
	})
	s.vlog(req, "jev routed the hook rejection to %s", route.Action)
	return route.Action
}

// hookRouteFloorOf reads the router's own confidence floor, when it publishes
// one.
func hookRouteFloorOf(router commit.FailureRouter) (float64, bool) {
	if floored, ok := router.(interface{ HookRouteFloor() float64 }); ok {
		return floored.HookRouteFloor(), true
	}
	return 0, false
}

// maxHookRewrites bounds how often one group may be drafted again after a
// routing decision asked for a rewrite, and maxTotalHookRewrites bounds the
// rewrites of a whole run. The per-run cap matters because a router that always
// says "rewrite" would otherwise redraft the same group without end.
const (
	maxHookRewrites      = 1
	maxTotalHookRewrites = 2
)

// groupKey identifies a group by its file set, independent of order, so the
// same group is recognized however the loop reaches it again.
func groupKey(files []string) string {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	return strings.Join(sorted, "\x1f")
}

// decidePrefix asks the judge for the conventional-commit type and scope of one
// change and returns the scope the generator must be told to pin.
//
// Only the scope is pinned. Measurement over a repository's own history put the
// scope in agreement 92 percent of the time with every disagreement arriving
// around 0.5 confidence, so a 0.7 floor separates it cleanly.
//
// The type is never pinned, and that is a measured decision rather than a
// cautious one: the type agreed in 58 to 67 percent of cases and its
// disagreements arrived at 0.81 to 0.94 confidence. The wrong answers are the
// confident ones, so no floor separates right from wrong and pinning the type at
// any threshold would write confidently wrong history. The type stays with the
// model until a measurement shows a floor that works.
//
// The judge refuses a language it has not been measured on, so a non-English
// message keeps its provider's prefix. The gate lives in the domain so the call
// is skipped rather than made and refused.
func (s *CommitService) decidePrefix(ctx context.Context, req CommitRequest, groupDiff *diff.StagedDiff) (pinScope string) {
	session := s.decisions.Begin()
	if s.judge == nil || !session.Take(SeamTypeScope) {
		return ""
	}
	scopeFloor, ok := scopeFloorOf(s.judge)
	if !ok {
		return ""
	}
	language := ""
	if req.Config != nil {
		language = req.Config.Language
	}
	if !commit.JudgeSupportsLanguage(language) {
		s.vlog(req, "type and scope judgment skipped: language %q is not measured", language)
		return ""
	}

	started := time.Now()
	verdict, err := s.judge.JudgeTypeScope(ctx, commit.TypeScopeRequest{
		Files:    fileChanges(groupDiff),
		Intent:   req.Intent,
		Scopes:   scopeMap(req.Config.Scopes),
		Language: language,
	})
	if err != nil {
		session.Report(started, decision.Observation{Seam: SeamTypeScope, Err: err.Error()})
		return ""
	}

	scopeAllowed := session.AdoptAt(SeamTypeScope, verdict.ScopeConfidence, scopeFloor)
	pinScope = verdict.PinnedScope(scopeAllowed)

	// The observation records the confidence of the part that was acted on, so a
	// log line never shows a low confidence next to a high one for one answer.
	observed := verdict.TypeConfidence
	if scopeAllowed {
		observed = verdict.ScopeConfidence
	}
	decided := fmt.Sprintf("type=%s scope=%s", orNone(verdict.Type), orNone(verdict.Scope))
	if scopeAllowed {
		decided += " (scope pinned)"
	}
	s.vlog(req, "jev answered %s: type %.2f scope %.2f", decided, verdict.TypeConfidence, verdict.ScopeConfidence)
	session.Report(started, decision.Observation{
		Seam:       SeamTypeScope,
		Decision:   decided,
		Confidence: observed,
		Adopted:    scopeAllowed,
	})
	return pinScope
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

// scopeFloorOf reads the seam's own scope floor. A judge that publishes none is
// trusted no further than the policy floor.
func scopeFloorOf(judge commit.TypeScopeJudge) (float64, bool) {
	scoped, ok := judge.(interface{ ScopeFloor() float64 })
	if !ok {
		return 0, false
	}
	return scoped.ScopeFloor(), true
}

// fileChanges renders a diff as the per-file summary the judge reads. The diff
// body never leaves this function.
func fileChanges(d *diff.StagedDiff) []commit.FileChange {
	if d == nil {
		return nil
	}
	evidence := diffEvidence(d)
	out := make([]commit.FileChange, 0, len(d.Files))
	for _, file := range d.Files {
		change := commit.FileChange{Path: file}
		if ev := evidence[file]; ev != nil {
			change.Adds, change.Dels = ev.adds, ev.dels
		}
		out = append(out, change)
	}
	return out
}

// plan produces the commit grouping. The System One decider answers it when the
// layer is on and confident; every other case, including a shadow run, keeps the
// planner's grouping. A shadow run records both so the difference is measurable
// before the decider is trusted.
func (s *CommitService) plan(ctx context.Context, req CommitRequest, staged, unstaged *diff.StagedDiff, allowed map[string]bool, renamed []diff.Rename) (*commit.CommitPlan, error) {
	session := s.decisions.Begin()
	judged, shadow := s.judgeGrouping(ctx, session, req, staged, unstaged)

	if judged != nil {
		// The detected renames still travel with the plan: a rename pair belongs
		// in one commit whatever the judgment decided.
		s.normalizePlan(judged, allowed, renamed, req)
		return judged, nil
	}

	plan, err := s.runPlan(ctx, req, commit.PlanRequest{StagedDiff: staged, UnstagedDiff: unstaged, Intent: req.Intent, Config: req.Config, MaxPlanFiles: req.MaxPlanFiles})
	if err != nil {
		return nil, err
	}
	if shadow != nil {
		shadow.compare(planGroups(plan))
		session.Report(shadow.started, shadow.Observation)
	}
	return plan, nil
}

// judgeGrouping asks the decider which candidate buckets belong together. It
// returns the plan when the answer may decide, and otherwise the shadow
// observation to record against the planner's own grouping.
//
// The buckets are built from every changed file, not from the staged set alone,
// because a grouping covers staged and unstaged work together.
func (s *CommitService) judgeGrouping(ctx context.Context, session *Session, req CommitRequest, staged, unstaged *diff.StagedDiff) (*commit.CommitPlan, *pendingGrouping) {
	if s.decider == nil || !session.Take(SeamGrouping) {
		return nil, nil
	}

	// The planning step has file lists but no diff body, so the line counts come
	// from numstat. A failure here costs the judgment its line counts and
	// nothing else, so it is tolerated.
	numstat, err := s.git.StagedDiffNumStat(ctx)
	if err != nil {
		s.vlog(req, "grouping judgment without line counts: %v", err)
	}
	buckets := buildBuckets([]*diff.StagedDiff{staged, unstaged}, numstat, maxCommitGroups)
	if len(buckets) < 2 {
		return nil, nil
	}
	bucketReq := commit.GroupDecisionRequest{Buckets: buckets, Scopes: scopeMap(req.Config.Scopes)}

	started := time.Now()
	verdict, err := s.decider.DecideGroups(ctx, bucketReq)
	if err != nil {
		session.Report(started, decision.Observation{Seam: SeamGrouping, Err: err.Error()})
		return nil, nil
	}

	groups, problems := verdict.DecidedGroups(bucketReq)
	for _, problem := range problems {
		s.vlog(req, "grouping decision: %s", problem)
	}
	if len(groups) == 0 {
		// A decision code cannot execute is no decision: keep the planner.
		session.Report(started, decision.Observation{Seam: SeamGrouping, Err: "decision named no executable group"})
		return nil, nil
	}

	signature := groupingSignature(groups)
	confidence := verdict.Confidence
	if !session.AdoptAt(SeamGrouping, confidence, groupingFloorOf(s.decider)) {
		return nil, &pendingGrouping{started: started, Observation: decision.Observation{
			Seam: SeamGrouping, Decision: signature, Confidence: confidence,
		}}
	}

	plan := &commit.CommitPlan{}
	for _, files := range groups {
		plan.Groups = append(plan.Groups, commit.CommitGroup{Files: files})
	}
	session.Report(started, decision.Observation{
		Seam: SeamGrouping, Decision: signature, Confidence: confidence, Adopted: true,
	})
	s.vlog(req, "jev grouping measured but not adopted: %s", signature)
	return plan, nil
}

// groupingFloorOf reads the seam's own floor. A decider that publishes none is
// trusted no further than the policy floor.
func groupingFloorOf(decider commit.GroupDecider) float64 {
	if floored, ok := decider.(interface{ GroupFloor() float64 }); ok {
		return floored.GroupFloor()
	}
	return 0
}

// pendingGrouping carries a shadowed grouping until the planner's own grouping
// exists to compare it with.
type pendingGrouping struct {
	started     time.Time
	Observation decision.Observation
}

func planGroups(plan *commit.CommitPlan) [][]string {
	var out [][]string
	for _, g := range plan.Groups {
		out = append(out, g.Files)
	}
	return out
}

func (p *pendingGrouping) compare(plan [][]string) {
	baseline := groupingSignature(plan)
	p.Observation.Baseline = baseline
	switch {
	case p.Observation.Decision == "" || baseline == "":
		p.Observation.Agreement = decision.AgreementNone
	case p.Observation.Decision == baseline:
		p.Observation.Agreement = decision.AgreementAgree
	default:
		p.Observation.Agreement = decision.AgreementDisagree
	}
}

func (s *CommitService) normalizePlan(plan *commit.CommitPlan, allowed map[string]bool, renamed []diff.Rename, req CommitRequest) {
	if n := filterPlanFiles(plan, allowed); n > 0 {
		s.vlog(req, "dropped %d hallucinated file(s) from plan", n)
	}
	if len(plan.Groups) > maxCommitGroups {
		s.out(req, "Warning: commit plan exceeds group limit (%d > %d), capping", len(plan.Groups), maxCommitGroups)
		plan.Groups = plan.Groups[:maxCommitGroups]
	}
	appendPassthroughFiles(plan, allowed)
	coLocateRenames(plan, renamed, allowed)
}

func (s *CommitService) refreshScopesAndPlan(ctx context.Context, req CommitRequest, staged, unstaged *diff.StagedDiff, allowed map[string]bool, renamed []diff.Rename, plan *commit.CommitPlan) error {
	s.out(req, "Refreshing scopes...")
	newScopes, err := s.scopeSvc.Generate(ctx, 200, req.Config.Scopes)
	if err != nil {
		s.vlog(req, "scope refresh failed (continuing with current plan): %v", err)
		return nil
	}
	req.Config.Scopes = newScopes
	s.out(req, "Scopes updated (in-memory): %v, re-planning...", req.Config.ScopeNames())
	updated, err := s.runPlan(ctx, req, commit.PlanRequest{StagedDiff: staged, UnstagedDiff: unstaged, Intent: req.Intent, Config: req.Config, MaxPlanFiles: req.MaxPlanFiles})
	if err != nil {
		return fmt.Errorf("re-plan after scope refresh: %w", err)
	}
	*plan = *updated
	s.normalizePlan(plan, allowed, renamed, req)
	return nil
}

type generatedGroupMessage struct {
	msg          *commit.CommitMessage
	assembled    string
	preTrailer   string
	hookFeedback string
	hookOutcome  string
	passed       bool
}

func (s *CommitService) commitGroups(ctx context.Context, req CommitRequest, plan *commit.CommitPlan, allowed map[string]bool, renames []diff.Rename) (_ *CommitResult, retErr error) {
	remaining := append([]commit.CommitGroup(nil), plan.Groups...)
	totalGroups := len(plan.Groups)
	commitWord := "commits"
	if totalGroups == 1 {
		commitWord = "commit"
	}
	s.out(req, "Planning commits: done (%d %s).", totalGroups, commitWord)

	var committed []SingleCommitResult
	committedFiles := make(map[string]bool)
	rePlanCount := 0
	// rewrites bounds how often one group may be drafted again because the hook
	// rejected its wording. It is keyed by the group's files, because the loop
	// counter advances on every iteration: a counter key would let the same
	// group be drafted again without bound.
	rewrites := map[string]int{}
	totalRewrites := 0
	var inheritedFeedback string

	defer func() {
		if retErr != nil {
			s.restoreUncommittedFiles(allowed, committedFiles)
		}
	}()

	groupIdx := 0
	for len(remaining) > 0 {
		group := remaining[0]
		remaining = remaining[1:]
		groupIdx++

		groupDiff, genDiff, err := s.prepareGroup(ctx, req, group, groupIdx, totalGroups)
		if err != nil {
			return nil, err
		}
		if groupDiff == nil {
			continue
		}

		attempt, err := s.generateGroupMessage(ctx, req, genDiff, groupDiff, groupIdx, totalGroups, inheritedFeedback)
		inheritedFeedback = ""
		if err != nil {
			return nil, err
		}
		if !attempt.passed {
			if rePlanCount >= maxRePlans {
				return nil, &HookBlockedError{LastMessage: attempt.preTrailer, Reason: attempt.hookFeedback}
			}
			inheritedFeedback = attempt.hookFeedback
			switch s.routeHookFailure(ctx, req, group, remaining, attempt) {
			case commit.FailureGiveUp:
				return nil, &HookBlockedError{LastMessage: attempt.preTrailer, Reason: attempt.hookFeedback}
			case commit.FailureRewrite:
				// The wording is at fault, so the same group is drafted again.
				// One rewrite per group, and a small total, keep a rejected hook
				// from looping.
				key := groupKey(group.Files)
				if rewrites[key] >= maxHookRewrites || totalRewrites >= maxTotalHookRewrites {
					s.vlog(req, "hook rewrite budget spent; re-planning instead")
					break
				}
				rewrites[key]++
				totalRewrites++
				remaining = append([]commit.CommitGroup{group}, remaining...)
				continue
			}
			rePlanCount++
			newPlan, err := s.replanAfterHook(ctx, req, group, remaining, allowed, committedFiles, renames)
			if err != nil {
				return nil, err
			}
			remaining = newPlan.Groups
			continue
		}

		result := SingleCommitResult{
			Title:       attempt.msg.Title,
			Message:     attempt.preTrailer,
			Explanation: attempt.msg.Explanation,
			Files:       group.Files,
			HookOutcome: attempt.hookOutcome,
		}
		if req.DryRun {
			committed = append(committed, result)
			markCommitted(committedFiles, group.Files)
			continue
		}

		gitOut, err := s.git.Commit(ctx, attempt.assembled)
		if err != nil {
			if errors.Is(err, pkgerrors.ErrNothingToCommit) {
				s.vlog(req, "skipping group (nothing to commit at commit time): %v", group.Files)
				continue
			}
			return nil, err
		}
		result.GitOutput = gitOut
		if hash, hashErr := s.git.CommitHash(ctx); hashErr == nil {
			result.SHA = hash
		}
		committed = append(committed, result)
		markCommitted(committedFiles, group.Files)
	}

	if len(committed) == 0 && !req.DryRun {
		return nil, fmt.Errorf("no changes committed: all %d planned group(s) skipped (no diff after staging)", len(plan.Groups))
	}
	return &CommitResult{Commits: committed, DryRun: req.DryRun}, nil
}

func (s *CommitService) prepareGroup(ctx context.Context, req CommitRequest, group commit.CommitGroup, groupIdx, totalGroups int) (*diff.StagedDiff, *diff.StagedDiff, error) {
	if err := s.git.UnstageAll(ctx); err != nil {
		return nil, nil, fmt.Errorf("unstage all: %w", err)
	}
	if err := s.git.StageFiles(ctx, group.Files); err != nil {
		return nil, nil, fmt.Errorf("stage files %v: %w", group.Files, err)
	}
	groupDiff, err := s.git.StagedDiff(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("staged diff for group: %w", err)
	}
	if len(groupDiff.Files) == 0 {
		s.vlog(req, "skipping group (no diff after staging): %v", group.Files)
		return nil, nil, nil
	}

	genDiff := groupDiff
	if s.filter != nil {
		if filtered, filterErr := s.filter.Filter(ctx, groupDiff); filterErr == nil {
			genDiff = filtered
		}
	}
	if s.truncator == nil {
		return groupDiff, genDiff, nil
	}

	maxBytes := effectiveMaxBytes(req.MaxBytes)
	truncated, didTruncate, err := s.truncator.Truncate(ctx, genDiff, req.MaxLines, maxBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("truncate group diff: %w", err)
	}
	if didTruncate {
		s.vlog(req, "group diff truncated (%s)", truncationLimitDesc(req.MaxLines, maxBytes))
	}
	genDiff = truncated
	if !didTruncate {
		return groupDiff, genDiff, nil
	}

	if len(genDiff.Files) == 1 && len(genDiff.Content) == maxBytes {
		stat, statErr := s.git.StagedDiffNumStat(ctx)
		if statErr == nil {
			synopsis := buildSynopsis(genDiff.Files[0], stat, len(groupDiff.Content), maxBytes)
			genDiff = &diff.StagedDiff{Files: genDiff.Files, Content: synopsis, Lines: strings.Count(synopsis, "\n")}
			s.out(req, "Warning: commit %d/%d: falling back to diff synopsis for %s", groupIdx, totalGroups, genDiff.Files[0])
		} else {
			s.vlog(req, "stat fallback failed (continuing with truncated diff): %v", statErr)
			s.out(req, "Warning: commit %d/%d: diff exceeds limit, truncating to %d bytes", groupIdx, totalGroups, maxBytes)
		}
	} else {
		s.out(req, "Warning: commit %d/%d: diff exceeds limit, truncating to %d bytes", groupIdx, totalGroups, maxBytes)
	}
	return groupDiff, genDiff, nil
}

func (s *CommitService) generateGroupMessage(ctx context.Context, req CommitRequest, genDiff, groupDiff *diff.StagedDiff, groupIdx, totalGroups int, inheritedFeedback string) (*generatedGroupMessage, error) {
	hookFeedback := inheritedFeedback
	var previousMessage string
	var preTrailer string
	// A decided prefix stays fixed across retries: the hook rejected the wording
	// or the grouping, and neither is fixed by changing the type or scope. It is
	// decided before the first draft, because the first draft is the one the
	// prefix applies to.
	pinnedScope := s.decidePrefix(ctx, req, groupDiff)
	for attempt := 1; attempt <= maxHookRetries; attempt++ {
		if attempt == 1 {
			s.out(req, "Drafting message: %d/%d...", groupIdx, totalGroups)
		}
		msg, err := s.gen.Generate(ctx, commit.GenerateRequest{
			Diff: genDiff, Intent: req.Intent, Config: req.Config,
			HookFeedback: hookFeedback, PreviousMessage: previousMessage,
			PinnedScope: pinnedScope,
		})
		if err != nil {
			return nil, fmt.Errorf("generate commit message: %w", err)
		}
		s.vlog(req, "LLM response received")

		assembled := msg.Title
		if body := msg.Body(); body != "" {
			assembled += "\n\n" + body
		}
		preTrailer = assembled
		if len(req.Trailers) > 0 {
			assembled, err = s.git.FormatTrailers(ctx, assembled, req.Trailers)
			if err != nil {
				return nil, fmt.Errorf("format trailers: %w", err)
			}
		}

		if len(req.Config.Hooks) == 0 && !req.Config.RequireModelCoAuthor {
			return &generatedGroupMessage{msg: msg, assembled: assembled, preTrailer: preTrailer, hookOutcome: "skipped", passed: true}, nil
		}
		hookResult, err := s.hookExec.Execute(ctx, req.Config.Hooks, hook.HookInput{
			Diff: groupDiff.Content, CommitMessage: assembled, Intent: req.Intent,
			StagedFiles: groupDiff.Files, Config: *req.Config,
		})
		if err != nil {
			return nil, fmt.Errorf("hook execute: %w", err)
		}
		if hookResult.ExitCode == 0 {
			outcome := "skipped"
			if hooksAreEffective(req.Config.Hooks) || req.Config.RequireModelCoAuthor {
				outcome = "passed"
			}
			return &generatedGroupMessage{msg: msg, assembled: assembled, preTrailer: preTrailer, hookOutcome: outcome, passed: true}, nil
		}
		if attempt < maxHookRetries {
			s.out(req, "Warning: hook rejected message, retrying... (attempt %d/%d)", attempt+1, maxHookRetries)
		}
		hookFeedback = hookResult.Stderr
		previousMessage = preTrailer
	}
	return &generatedGroupMessage{preTrailer: preTrailer, hookFeedback: hookFeedback}, nil
}

func (s *CommitService) replanAfterHook(ctx context.Context, req CommitRequest, group commit.CommitGroup, remaining []commit.CommitGroup, allowed map[string]bool, committedFiles map[string]bool, renames []diff.Rename) (*commit.CommitPlan, error) {
	var allFiles []string
	allFiles = append(allFiles, group.Files...)
	for _, pending := range remaining {
		allFiles = append(allFiles, pending.Files...)
	}
	newPlan, err := s.runPlan(ctx, req, commit.PlanRequest{
		StagedDiff: &diff.StagedDiff{Files: allFiles}, Intent: req.Intent,
		Config: req.Config, MaxPlanFiles: req.MaxPlanFiles,
	})
	if err != nil {
		return nil, fmt.Errorf("re-plan commits: %w", err)
	}
	rePlanAllowed := make(map[string]bool, len(allowed))
	for file := range allowed {
		if !committedFiles[file] {
			rePlanAllowed[file] = true
		}
	}
	if n := filterPlanFiles(newPlan, rePlanAllowed); n > 0 {
		s.vlog(req, "dropped %d hallucinated file(s) from hook re-plan", n)
	}
	if len(newPlan.Groups) > maxCommitGroups {
		s.vlog(req, "hook re-plan has %d groups — capping to %d", len(newPlan.Groups), maxCommitGroups)
		newPlan.Groups = newPlan.Groups[:maxCommitGroups]
	}
	appendPassthroughFiles(newPlan, rePlanAllowed)
	coLocateRenames(newPlan, renames, rePlanAllowed)
	return newPlan, nil
}

func (s *CommitService) restoreUncommittedFiles(allowed map[string]bool, committedFiles map[string]bool) {
	var toRestore []string
	for file := range allowed {
		if !committedFiles[file] {
			toRestore = append(toRestore, file)
		}
	}
	if len(toRestore) == 0 {
		return
	}
	sort.Strings(toRestore)
	_ = s.git.StageFiles(context.Background(), toRestore)
}

func markCommitted(committedFiles map[string]bool, files []string) {
	for _, file := range files {
		committedFiles[file] = true
	}
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
	preTrailer := assembled
	if len(req.Trailers) > 0 {
		assembled, err = s.git.FormatTrailers(ctx, assembled, req.Trailers)
		if err != nil {
			return nil, fmt.Errorf("format trailers: %w", err)
		}
	}

	result := SingleCommitResult{
		Title:       msg.Title,
		Message:     preTrailer,
		Explanation: msg.Explanation,
		Files:       amendDiff.Files,
		HookOutcome: "skipped",
	}
	if req.DryRun {
		return &CommitResult{Commits: []SingleCommitResult{result}, DryRun: true}, nil
	}
	gitOut, err := s.git.AmendCommit(ctx, assembled)
	if err != nil {
		return nil, err
	}
	result.GitOutput = gitOut
	if hash, hashErr := s.git.CommitHash(ctx); hashErr == nil {
		result.SHA = hash
	}
	return &CommitResult{Commits: []SingleCommitResult{result}}, nil
}

// filterPlanFiles removes from each CommitGroup any file not in allowed, then
// drops groups with no remaining files. Returns the number of dropped files.
func filterPlanFiles(plan *commit.CommitPlan, allowed map[string]bool) int {
	filtered := 0
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
	}
	dropEmptyGroups(plan)
	return filtered
}

// dropEmptyGroups removes commit groups left with no files, compacting the
// slice in place.
func dropEmptyGroups(plan *commit.CommitPlan) {
	kept := plan.Groups[:0]
	for i := range plan.Groups {
		if len(plan.Groups[i].Files) > 0 {
			kept = append(kept, plan.Groups[i])
		}
	}
	plan.Groups = kept
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

// coLocateRenames forces both paths of each detected rename into the same
// commit group. A move whose deletion lands in one commit and whose addition
// lands in another leaves an intermediate commit where the file exists in
// neither or both locations, and git can no longer render it as a rename — the
// exact "one move, two commits" split this guards against. The old path is
// moved into the group that holds the new path, because the new path's content
// is what the per-group message describes; a group emptied by the move is
// dropped. Pairs with either side outside allowed (e.g. already committed on a
// hook re-plan) are skipped so committed files are never disturbed.
func coLocateRenames(plan *commit.CommitPlan, renames []diff.Rename, allowed map[string]bool) {
	if len(plan.Groups) == 0 || len(renames) == 0 {
		return
	}
	groupOf := make(map[string]int)
	for gi := range plan.Groups {
		for _, f := range plan.Groups[gi].Files {
			groupOf[f] = gi
		}
	}
	// The caller always runs appendPassthroughFiles first, so both paths of an
	// allowed rename are already in some group — the ,ok lookups still guard
	// the map's zero-value (a missing key reads as group 0), so a pair that is
	// only partially grouped is safely skipped rather than mis-placed.
	for _, r := range renames {
		if !allowed[r.Old] || !allowed[r.New] {
			continue
		}
		newGi, newOK := groupOf[r.New]
		oldGi, oldOK := groupOf[r.Old]
		if newOK && oldOK && newGi != oldGi {
			plan.Groups[oldGi].Files = removeString(plan.Groups[oldGi].Files, r.Old)
			plan.Groups[newGi].Files = append(plan.Groups[newGi].Files, r.Old)
			groupOf[r.Old] = newGi
		}
	}
	dropEmptyGroups(plan)
}

// removeString returns files with v removed, reusing the backing array (the
// caller owns the slice). A group's file list has no duplicates, so removing
// every match is equivalent to removing the one occurrence.
func removeString(files []string, v string) []string {
	out := files[:0]
	for _, f := range files {
		if f != v {
			out = append(out, f)
		}
	}
	return out
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

// hooksAreEffective reports whether any configured hook performs real validation
// — i.e. is something other than the no-op "" / "empty" sentinels. Used to label
// a commit's hook_outcome as "passed" (a real hook accepted it) vs "skipped"
// (no validation ran).
func hooksAreEffective(hooks []string) bool {
	for _, h := range hooks {
		if h != "" && h != "empty" {
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
