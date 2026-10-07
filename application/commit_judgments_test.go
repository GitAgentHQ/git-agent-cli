package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/decision"
	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/hook"
	"github.com/gitagenthq/git-agent/domain/project"
)

// stubGroupDecider implements commit.GroupDecider.
type stubGroupDecider struct {
	verdict *commit.GroupDecision
	err     error
	req     commit.GroupDecisionRequest
	calls   int
}

func (d *stubGroupDecider) DecideGroups(ctx context.Context, req commit.GroupDecisionRequest) (*commit.GroupDecision, error) {
	d.req = req
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	return d.verdict, nil
}

// stubTypeScopeJudge implements commit.TypeScopeJudge and publishes a floor.
type stubTypeScopeJudge struct {
	verdict *commit.TypeScopeVerdict
	err     error
	req     commit.TypeScopeRequest
	floor   float64
	calls   int
}

func (j *stubTypeScopeJudge) JudgeTypeScope(ctx context.Context, req commit.TypeScopeRequest) (*commit.TypeScopeVerdict, error) {
	j.req = req
	j.calls++
	if j.err != nil {
		return nil, j.err
	}
	return j.verdict, nil
}

// ScopeFloor reuses the stub's floor, which keeps one number in the test.
func (j *stubTypeScopeJudge) ScopeFloor() float64 { return j.floor }

// stubFailureRouter implements commit.FailureRouter.
type stubFailureRouter struct {
	route *commit.FailureRoute
	err   error
	calls int
}

func (r *stubFailureRouter) RouteFailure(ctx context.Context, req commit.FailureRouteRequest) (*commit.FailureRoute, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.route, nil
}

func onLayer(log *strings.Builder) *application.Decisions {
	return application.NewDecisions(decision.ModeOn, 0.5, 8, nil, log)
}

// twoDirDiff returns a staged change in application/ and an unstaged one in cmd/,
// which is the smallest change set that produces two candidate buckets.
func twoDirDiff() *diff.StagedDiff {
	return &diff.StagedDiff{
		Files: []string{"application/commit_service.go", "cmd/commit.go", "cmd/root.go"},
		Content: "diff --git a/application/commit_service.go\n" +
			"--- a/application/commit_service.go\n" +
			"+++ b/application/commit_service.go\n" +
			"@@ -10,3 +10,4 @@ func (s *CommitService) commitGroups\n" +
			"+\tnewLine()\n" +
			"-\toldLine()\n" +
			"diff --git a/cmd/commit.go\n" +
			"--- a/cmd/commit.go\n" +
			"+++ b/cmd/commit.go\n" +
			"@@ -3,2 +3,3 @@ func runCommit\n" +
			"+\tflag()\n" +
			"diff --git a/cmd/root.go\n" +
			"--- a/cmd/root.go\n" +
			"+++ b/cmd/root.go\n" +
			"@@ -1,2 +1,2 @@ func main\n" +
			"+\tmain()\n",
	}
}

// newJudgmentGit returns a repository with one file per two directories.
func newJudgmentGit() *mockCommitGitClient {
	return &mockCommitGitClient{
		repoRoot:        "/repo",
		stagedDiff:      twoDirDiff(),
		unstagedDiff:    twoDirDiff(),
		allChangedFiles: []string{"application/commit_service.go", "cmd/commit.go", "cmd/root.go"},
		stagedDiffNumStat: func(context.Context) (string, error) {
			return "2\t1\tapplication/commit_service.go\n3\t0\tcmd/commit.go\n1\t1\tcmd/root.go\n", nil
		},
	}
}

// commitRequestForJudgment is the request the judgment tests share.
func commitRequestForJudgment() application.CommitRequest {
	return application.CommitRequest{
		Config: &project.Config{
			Hooks:  []string{"conventional"},
			Scopes: []project.Scope{{Name: "app", Description: "services in application/"}, {Name: "cli", Description: "commands in cmd/"}},
		},
	}
}

// newJudgmentCommitService builds the service under test with the seams the
// caller needs and the rest left nil, so an unwired seam falls back to the
// existing path instead of being exercised.
func newJudgmentCommitService(
	git *mockCommitGitClient,
	planner commit.CommitPlanner,
	gen commit.CommitMessageGenerator,
	hookExec hook.HookExecutor,
	decider commit.GroupDecider,
	judge commit.TypeScopeJudge,
	router commit.FailureRouter,
) *application.CommitService {
	if hookExec == nil {
		hookExec = noopHook()
	}
	return application.NewCommitService(gen, planner, git, hookExec, nil, nil, nil, nil).
		WithJudgments(decider, judge, router)
}

func TestCommitService_GroupingJudgmentDecidesThePlan(t *testing.T) {
	// Given a decider that keeps the two directories apart.
	decider := &stubGroupDecider{verdict: &commit.GroupDecision{GroupBuckets: [][]string{{"a"}, {"b"}}, Confidence: 0.9}}
	planner := &recordingPlanner{}
	git := newJudgmentGit()
	svc := newJudgmentCommitService(git, planner, &mockCommitGenerator{msg: defaultMsg()}, nil, decider, nil, nil).
		WithDecisions(onLayer(nil))

	res, err := svc.Commit(context.Background(), commitRequestForJudgment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Commits) != 2 {
		t.Fatalf("expected two commits from two buckets, got %d", len(res.Commits))
	}
	if planner.calls != 0 {
		t.Errorf("expected the planner to be skipped, got %d calls", planner.calls)
	}
	if len(decider.req.Buckets) != 2 {
		t.Fatalf("expected two candidate buckets, got %+v", decider.req.Buckets)
	}
	if decider.req.Buckets[0].Adds == 0 || len(decider.req.Buckets[0].Files) == 0 {
		t.Errorf("expected the bucket to carry a structured summary, got %+v", decider.req.Buckets[0])
	}
}

func TestCommitService_GroupingEvidenceNeverCarriesTheDiffBody(t *testing.T) {
	decider := &stubGroupDecider{verdict: &commit.GroupDecision{GroupBuckets: [][]string{{"a"}, {"b"}}, Confidence: 0.9}}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{}, &mockCommitGenerator{msg: defaultMsg()}, nil, decider, nil, nil).
		WithDecisions(onLayer(nil))

	if _, err := svc.Commit(context.Background(), commitRequestForJudgment()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rendered := renderRequest(decider.req)
	if strings.Contains(rendered, "newLine()") || strings.Contains(rendered, "func main") {
		t.Errorf("expected no diff body in the judgment state, got %q", rendered)
	}
}

func TestCommitService_ShadowGroupingKeepsThePlannerAndRecordsTheDifference(t *testing.T) {
	// Given a layer in shadow mode and a decider that merges both buckets into
	// one group while the planner splits them.
	decider := &stubGroupDecider{verdict: &commit.GroupDecision{GroupBuckets: [][]string{{"a", "b"}}}}
	planner := &recordingPlanner{groups: [][]string{{"application/commit_service.go"}, {"cmd/commit.go", "cmd/root.go"}}}
	var recorded []decision.Observation
	rec := decision.RecorderFunc(func(o decision.Observation) { recorded = append(recorded, o) })
	layer := application.NewDecisions(decision.ModeShadow, 0.5, 8, rec, nil)
	svc := newJudgmentCommitService(newJudgmentGit(), planner, &mockCommitGenerator{msg: defaultMsg()}, nil, decider, nil, nil).
		WithDecisions(layer)

	if _, err := svc.Commit(context.Background(), commitRequestForJudgment()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if planner.calls != 1 {
		t.Errorf("expected the planner to decide in shadow mode, got %d calls", planner.calls)
	}
	found := false
	for _, o := range recorded {
		if o.Seam != application.SeamGrouping {
			continue
		}
		found = true
		if o.Adopted {
			t.Error("expected a shadowed grouping never to be adopted")
		}
		if o.Agreement != decision.AgreementDisagree {
			t.Errorf("expected the disagreement to be recorded, got %q", o.Agreement)
		}
	}
	if !found {
		t.Errorf("expected a grouping observation, got %v", recorded)
	}
}

func TestCommitService_WeakTypeScopeVerdictLeavesTheTitleToTheModel(t *testing.T) {
	judge := &stubTypeScopeJudge{floor: 0.85, verdict: &commit.TypeScopeVerdict{
		Type: "fix", Scope: "app", TypeConfidence: 0.6, ScopeConfidence: 0.6,
	}}
	gen := &hookSequenceGenerator{}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{}, gen, nil, nil, judge, nil).
		WithDecisions(onLayer(nil))

	if _, err := svc.Commit(context.Background(), commitRequestForJudgment()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gen.reqs[0].DecidedPrefix != "" || gen.reqs[0].PinnedScope != "" {
		t.Errorf("expected nothing pinned below the floor, got prefix %q scope %q",
			gen.reqs[0].DecidedPrefix, gen.reqs[0].PinnedScope)
	}
}

func TestCommitService_TypeScopeJudgeIsSkippedForAnUnmeasuredLanguage(t *testing.T) {
	judge := &stubTypeScopeJudge{floor: 0.85, verdict: &commit.TypeScopeVerdict{
		Type: "fix", Scope: "app", TypeConfidence: 0.99, ScopeConfidence: 0.99,
	}}
	gen := &hookSequenceGenerator{}
	req := commitRequestForJudgment()
	req.Config = &project.Config{Scopes: []project.Scope{{Name: "app"}}, Language: "zh-CN"}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{}, gen, nil, nil, judge, nil).
		WithDecisions(onLayer(nil))

	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if judge.calls != 0 {
		t.Error("expected the judge to be skipped for an unmeasured language")
	}
	if gen.reqs[0].DecidedPrefix != "" {
		t.Errorf("expected no pinned prefix, got %q", gen.reqs[0].DecidedPrefix)
	}
}

func TestCommitService_HookRouteDecidesBetweenRewriteAndReplan(t *testing.T) {
	// Given a router that says the wording is at fault.
	router := &stubFailureRouter{route: &commit.FailureRoute{Action: commit.FailureRewrite, Confidence: 0.95}}
	planner := &recordingPlanner{}
	gen := &hookSequenceGenerator{msgs: []*commit.CommitMessage{badMessage()}}
	svc := newJudgmentCommitService(newJudgmentGit(), planner, gen, rejectingHook(3), nil, nil, router).
		WithDecisions(onLayer(nil))

	res, err := svc.Commit(context.Background(), commitRequestForJudgment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Commits) != 2 {
		t.Fatalf("expected both commits to land, got %d", len(res.Commits))
	}
	if router.calls != 1 {
		t.Errorf("expected one routing decision, got %d", router.calls)
	}
	if planner.calls != 1 {
		t.Errorf("expected the initial plan and no re-plan for a wording problem, got %d planner calls", planner.calls)
	}
}

func TestCommitService_HookRouteGiveUpStopsTheCommit(t *testing.T) {
	router := &stubFailureRouter{route: &commit.FailureRoute{Action: commit.FailureGiveUp, Confidence: 0.95}}
	gen := &hookSequenceGenerator{msgs: []*commit.CommitMessage{badMessage()}}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{}, gen, alwaysRejectHook{}, nil, nil, router).
		WithDecisions(onLayer(nil))

	_, err := svc.Commit(context.Background(), commitRequestForJudgment())
	if err == nil || !errors.Is(err, application.ErrHookBlocked) {
		t.Fatalf("expected the commit to be blocked, got %v", err)
	}
	if router.calls != 1 {
		t.Errorf("expected one routing decision, got %d", router.calls)
	}
}

func TestCommitService_JudgmentFailureKeepsTheExistingPath(t *testing.T) {
	// Given every judgment failing, the commit must still land on the old path.
	decider := &stubGroupDecider{err: errors.New("jev down")}
	judge := &stubTypeScopeJudge{floor: 0.85, err: errors.New("jev down")}
	router := &stubFailureRouter{err: errors.New("jev down")}
	planner := &recordingPlanner{groups: [][]string{{"application/commit_service.go"}, {"cmd/commit.go", "cmd/root.go"}}}
	gen := &hookSequenceGenerator{msgs: []*commit.CommitMessage{goodMessage()}}
	svc := newJudgmentCommitService(newJudgmentGit(), planner, gen, nil, decider, judge, router).
		WithDecisions(onLayer(nil))

	res, err := svc.Commit(context.Background(), commitRequestForJudgment())
	if err != nil {
		t.Fatalf("a judgment failure must not break the commit, got %v", err)
	}
	if len(res.Commits) != 2 {
		t.Errorf("expected the planner's two commits, got %d", len(res.Commits))
	}
	if planner.calls != 1 {
		t.Errorf("expected the planner to run after the decider failed, got %d calls", planner.calls)
	}
	if gen.reqs[0].DecidedPrefix != "" {
		t.Errorf("expected no pinned prefix after the judge failed, got %q", gen.reqs[0].DecidedPrefix)
	}
}

func TestCommitService_LayerOffRunsNoJudgment(t *testing.T) {
	decider := &stubGroupDecider{err: errors.New("must not run")}
	judge := &stubTypeScopeJudge{err: errors.New("must not run")}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{}, &mockCommitGenerator{msg: defaultMsg()}, nil, decider, judge, nil).
		WithDecisions(application.NewDecisions(decision.ModeOff, 0.5, 8, nil, nil))

	if _, err := svc.Commit(context.Background(), commitRequestForJudgment()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decider.calls != 0 || judge.calls != 0 {
		t.Errorf("expected no judgment in the off mode, got decider=%d judge=%d", decider.calls, judge.calls)
	}
}

func TestCommitService_SpentBudgetSkipsTheJudgment(t *testing.T) {
	decider := &stubGroupDecider{verdict: &commit.GroupDecision{GroupBuckets: [][]string{{"a", "b"}}}}
	judge := &stubTypeScopeJudge{floor: 0.85, verdict: &commit.TypeScopeVerdict{Type: "feat", TypeConfidence: 0.99, ScopeConfidence: 0.99}}
	layer := application.NewDecisions(decision.ModeOn, 0.5, 1, nil, nil)
	layer.Begin().Take("warmup")
	gen := &hookSequenceGenerator{}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{}, gen, nil, decider, judge, nil).
		WithDecisions(layer)

	if _, err := svc.Commit(context.Background(), commitRequestForJudgment()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decider.calls != 0 || judge.calls != 0 {
		t.Errorf("expected a spent budget to skip every judgment, got decider=%d judge=%d", decider.calls, judge.calls)
	}
}

// recordingPlanner implements commit.CommitPlanner and counts its calls, so a
// test can tell whether a judgment replaced the planner.
type recordingPlanner struct {
	groups [][]string
	calls  int
}

func (p *recordingPlanner) Plan(context.Context, commit.PlanRequest) (*commit.CommitPlan, error) {
	p.calls++
	groups := p.groups
	if len(groups) == 0 {
		// The default planner answer splits one directory per commit.
		groups = [][]string{{"application/commit_service.go"}, {"cmd/commit.go", "cmd/root.go"}}
	}
	plan := &commit.CommitPlan{}
	for _, files := range groups {
		plan.Groups = append(plan.Groups, commit.CommitGroup{Files: files})
	}
	return plan, nil
}

// hookSequenceGenerator is a generator that records every request and answers
// from a fixed script of messages, which is how the retry and routing paths are
// driven. The first len(msgs)-1 messages are returned once each, then the last
// one repeats.
type hookSequenceGenerator struct {
	reqs []*commit.GenerateRequest
	msgs []*commit.CommitMessage
}

func (g *hookSequenceGenerator) Generate(_ context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error) {
	g.reqs = append(g.reqs, &req)
	if len(g.msgs) == 0 {
		return goodMessage(), nil
	}
	idx := len(g.reqs) - 1
	if idx >= len(g.msgs) {
		idx = len(g.msgs) - 1
	}
	return g.msgs[idx], nil
}

// rejectingHook rejects the first n executions and accepts the rest, which is
// how a test drives the retry and routing paths.
func rejectingHook(n int) hook.HookExecutor {
	results := make([]*hook.HookResult, 0, n+1)
	for i := 0; i < n; i++ {
		results = append(results, &hook.HookResult{ExitCode: 1, Stderr: "title is not a conventional commit"})
	}
	return &sequenceHookExecutor{results: append(results, &hook.HookResult{ExitCode: 0})}
}

// alwaysRejectHook rejects every execution, so a rejection never clears.
type alwaysRejectHook struct{}

func (alwaysRejectHook) Execute(context.Context, []string, hook.HookInput) (*hook.HookResult, error) {
	return &hook.HookResult{ExitCode: 1, Stderr: "title is not a conventional commit"}, nil
}

// goodMessage is a message the conventional hook accepts.
func goodMessage() *commit.CommitMessage {
	return &commit.CommitMessage{Title: "feat(app): complete the judgment work"}
}

// badMessage is a message the conventional hook rejects: no conventional prefix.
func badMessage() *commit.CommitMessage {
	return &commit.CommitMessage{Title: "bad title without a type"}
}

// pannedMessage pairs a rejected message with the one that follows a rewrite.
func pannedMessage() *hookSequenceGenerator {
	return &hookSequenceGenerator{msgs: []*commit.CommitMessage{badMessage(), badMessage(), badMessage(), goodMessage()}}
}

func renderRequest(req commit.GroupDecisionRequest) string {
	var b strings.Builder
	for _, bucket := range req.Buckets {
		b.WriteString(bucket.Label)
		b.WriteString(strings.Join(bucket.Files, ","))
		b.WriteString(strings.Join(bucket.Symbols, ","))
	}
	return b.String()
}

var _ = hook.HookInput{}
var _ = time.Second

func TestCommitService_SeamStopsAfterRepeatedlyMissingItsFloor(t *testing.T) {
	// Given a judge that never reaches its floor, and four groups to judge.
	judge := &stubTypeScopeJudge{floor: 0.85, verdict: &commit.TypeScopeVerdict{
		Type: "chore", Scope: "app", TypeConfidence: 0.6, ScopeConfidence: 0.6,
	}}
	decider := &stubGroupDecider{verdict: &commit.GroupDecision{
		GroupBuckets: [][]string{{"a"}, {"b"}, {"c"}, {"d"}}, Confidence: 0.9,
	}}
	gen := &hookSequenceGenerator{msgs: []*commit.CommitMessage{goodMessage()}}
	layer := application.NewDecisions(decision.ModeOn, 0.5, 20, nil, nil)
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{
		groups: [][]string{
			{"application/commit_service.go"}, {"cmd/commit.go"},
			{"cmd/root.go"}, {"application/commit_service.go"},
		},
	}, gen, nil, decider, judge, nil).WithDecisions(layer)

	if _, err := svc.Commit(context.Background(), commitRequestForJudgment()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if judge.calls > application.SeamMissLimit {
		t.Errorf("expected the judge to stop after %d misses, got %d calls", application.SeamMissLimit, judge.calls)
	}
}

func TestCommitService_ShadowModeKeepsAskingAWeakSeam(t *testing.T) {
	// Shadow mode never adopts by design, so a miss must not stop the seam: the
	// measurement is the whole point of the mode.
	judge := &stubTypeScopeJudge{floor: 0.85, verdict: &commit.TypeScopeVerdict{
		Type: "chore", Scope: "app", TypeConfidence: 0.1, ScopeConfidence: 0.1,
	}}
	gen := &hookSequenceGenerator{msgs: []*commit.CommitMessage{goodMessage()}}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{
		groups: [][]string{{"application/commit_service.go"}, {"cmd/commit.go"}, {"cmd/root.go"}},
	}, gen, nil, nil, judge, nil).
		WithDecisions(application.NewDecisions(decision.ModeShadow, 0.5, 20, nil, nil))

	if _, err := svc.Commit(context.Background(), commitRequestForJudgment()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if judge.calls != 3 {
		t.Errorf("expected the judge to be asked once per group in shadow mode, got %d calls", judge.calls)
	}
}

func TestCommitService_RewriteRoutingTerminatesWhenTheHookNeverAccepts(t *testing.T) {
	// Given a router that always says "rewrite", a hook that always rejects,
	// and a generator that always returns a rejected message: the loop must end
	// with the hook reason rather than drafting the same group forever.
	router := &stubFailureRouter{route: &commit.FailureRoute{Action: commit.FailureRewrite, Confidence: 0.95}}
	gen := &hookSequenceGenerator{msgs: []*commit.CommitMessage{badMessage()}}
	svc := newJudgmentCommitService(newJudgmentGit(), &recordingPlanner{}, gen, alwaysRejectHook{}, nil, nil, router).
		WithDecisions(onLayer(nil))

	_, err := svc.Commit(context.Background(), commitRequestForJudgment())
	if err == nil || !errors.Is(err, application.ErrHookBlocked) {
		t.Fatalf("expected the commit to end with the hook reason, got %v", err)
	}
	// The invariant is a bounded number of drafts. One draft makes up to three
	// generation attempts, the service allows two rewrites in a whole run, and
	// two re-plans, so the loop cannot exceed fifteen attempts. Without a bound
	// it runs until the harness timeout.
	const maxAttempts = (2 + 1 + 2) * 3
	if len(gen.reqs) > maxAttempts {
		t.Errorf("expected at most %d drafts, got %d generation attempts", maxAttempts, len(gen.reqs))
	}
}

func TestCommitService_JudgedPlanKeepsRenamePairsTogether(t *testing.T) {
	// Given a detected rename whose two paths sit in different directories.
	git := newJudgmentGit()
	git.renames = []diff.Rename{{Old: "cmd/old.go", New: "application/new.go"}}
	decider := &stubGroupDecider{verdict: &commit.GroupDecision{
		GroupBuckets: [][]string{{"a", "b"}}, Confidence: 0.9,
	}}
	svc := newJudgmentCommitService(git, &recordingPlanner{},
		&mockCommitGenerator{msg: defaultMsg()}, nil, decider, nil, nil).
		WithDecisions(onLayer(nil))

	res, err := svc.Commit(context.Background(), commitRequestForJudgment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A rename pair belongs in one group, whatever the judgment said.
	for _, c := range res.Commits {
		hasOld, hasNew := false, false
		for _, f := range c.Files {
			hasOld = hasOld || f == "cmd/old.go"
			hasNew = hasNew || f == "application/new.go"
		}
		if hasOld != hasNew {
			t.Errorf("commit %q split a rename pair: %v", c.Title, c.Files)
		}
	}
}

func TestCommitService_GroupingJudgmentIsMeasuredButNotAdopted(t *testing.T) {
	// Given a decider that publishes the measured floor, which no answer can
	// reach, and a confident verdict.
	decider := &measuredGroupDecider{verdict: &commit.GroupDecision{
		GroupBuckets: [][]string{{"a"}, {"b"}}, Confidence: 0.99,
	}}
	planner := &recordingPlanner{}
	svc := newJudgmentCommitService(newJudgmentGit(), planner,
		&mockCommitGenerator{msg: defaultMsg()}, nil, decider, nil, nil).
		WithDecisions(onLayer(nil))

	res, err := svc.Commit(context.Background(), commitRequestForJudgment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The planner keeps the grouping: the judgment runs and decides nothing.
	if planner.calls != 1 {
		t.Errorf("expected the planner to decide, got %d calls", planner.calls)
	}
	if len(res.Commits) != 2 {
		t.Errorf("expected the planner's grouping, got %d commits", len(res.Commits))
	}
	if decider.calls != 1 {
		t.Errorf("expected the judgment to run once, got %d calls", decider.calls)
	}
}

// measuredGroupDecider publishes a floor no answer can reach, which is how a
// measured seam stays visible without deciding.
type measuredGroupDecider struct {
	verdict *commit.GroupDecision
	calls   int
}

func (d *measuredGroupDecider) DecideGroups(ctx context.Context, req commit.GroupDecisionRequest) (*commit.GroupDecision, error) {
	d.calls++
	return d.verdict, nil
}

func (d *measuredGroupDecider) GroupFloor() float64 { return 1.1 }
