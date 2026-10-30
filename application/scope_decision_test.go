package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/decision"
	"github.com/gitagenthq/git-agent/domain/project"
)

// onDecisions is the layer in the mode where a confident judgment decides.
func onDecisions() *application.Decisions {
	return application.NewDecisions(decision.ModeOn, 0.5, 8, nil, nil)
}

// stubDecider implements project.ScopeDecider and records the request it saw.
type stubDecider struct {
	decision *project.ScopeDecision
	err      error
	req      project.ScopeDecisionRequest
}

func (d *stubDecider) DecideScopes(ctx context.Context, req project.ScopeDecisionRequest) (*project.ScopeDecision, error) {
	d.req = req
	if d.err != nil {
		return nil, d.err
	}
	return d.decision, nil
}

func TestScopeService_Generate_DeciderOwnsTheScopeSet(t *testing.T) {
	// Given a decider that reuses one existing scope, creates one new scope,
	// and skips a directory that needs no scope.
	decider := &stubDecider{decision: &project.ScopeDecision{Confidence: 0.9, Assignments: []project.ScopeAssignment{
		{Dir: "application", Action: project.ScopeCreate, Scope: "app"},
		{Dir: "infrastructure", Action: project.ScopeReuse, Scope: "infra"},
		{Dir: ".github", Action: project.ScopeSkip},
	}}}
	llm := &mockLLMClient{}
	git := &mockGitReader{
		commits:   []string{"feat: init"},
		dirs:      []string{"application", "infrastructure", ".github"},
		files:     []string{"application/commit_service.go"},
		isGitRepo: true,
	}
	var warnings bytes.Buffer
	svc := application.NewScopeService(llm, git, decider, &warnings).WithDecisions(onDecisions())

	// When the service generates scopes.
	scopes, err := svc.Generate(context.Background(), 20, []project.Scope{{Name: "infra", Description: "adapters"}})

	// Then only the scope the decider asked to create is returned, and the
	// reused scope is left to the existing configuration.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 1 || scopes[0].Name != "app" {
		t.Fatalf("expected only the created scope [app], got %v", scopes)
	}
	if warnings.Len() != 0 {
		t.Errorf("expected no fallback warning, got %q", warnings.String())
	}
	if llm.scopes != nil {
		t.Errorf("expected the model not to be asked for the scope set, got %v", llm.scopes)
	}
	if decider.req.Dirs[0] != "application" || decider.req.ExistingScopes[0].Name != "infra" {
		t.Errorf("expected the decider to receive the repository evidence, got %+v", decider.req)
	}
}

func TestScopeService_Generate_DeciderAsksModelForDescriptionsOnly(t *testing.T) {
	// Given a decider that creates two scopes.
	decider := &stubDecider{decision: &project.ScopeDecision{Confidence: 0.9, Assignments: []project.ScopeAssignment{
		{Dir: "application", Action: project.ScopeCreate, Scope: "app"},
		{Dir: "cmd", Action: project.ScopeCreate, Scope: "cli"},
	}}}
	llm := &mockLLMClient{described: []project.Scope{
		{Name: "app", Description: "service orchestration in application/"},
		{Name: "cli", Description: "command surface in cmd/"},
	}}
	git := &mockGitReader{dirs: []string{"application", "cmd"}, isGitRepo: true}
	svc := application.NewScopeService(llm, git, decider, nil).WithDecisions(onDecisions())

	scopes, err := svc.Generate(context.Background(), 20, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %v", scopes)
	}
	for _, s := range scopes {
		if s.Description == "" {
			t.Errorf("expected scope %q to carry the model's description", s.Name)
		}
	}
}

func TestScopeService_Generate_DeciderFailureFallsBackToModel(t *testing.T) {
	// Given a decider that fails and a model that can answer.
	decider := &stubDecider{err: errors.New("jev unavailable")}
	llm := &mockLLMClient{scopes: []project.Scope{{Name: "app", Description: "services"}}}
	git := &mockGitReader{dirs: []string{"application"}, isGitRepo: true}
	var warnings bytes.Buffer
	svc := application.NewScopeService(llm, git, decider, &warnings).WithDecisions(onDecisions())

	scopes, err := svc.Generate(context.Background(), 20, nil)

	// Then the model decides the scope set and the fallback is reported.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 1 || scopes[0].Name != "app" {
		t.Fatalf("expected the model scope [app], got %v", scopes)
	}
	if !strings.Contains(warnings.String(), "jev unavailable") {
		t.Errorf("expected a fallback warning naming the failure, got %q", warnings.String())
	}
}

func TestScopeService_Generate_DescriptionFailureKeepsScope(t *testing.T) {
	// Given a decider that creates a scope and a model that cannot word it.
	decider := &stubDecider{decision: &project.ScopeDecision{Confidence: 0.9, Assignments: []project.ScopeAssignment{
		{Dir: "cmd", Action: project.ScopeCreate, Scope: "cli"},
	}}}
	llm := &mockLLMClient{describeErr: errors.New("no completion")}
	git := &mockGitReader{dirs: []string{"cmd"}, isGitRepo: true}
	svc := application.NewScopeService(llm, git, decider, nil).WithDecisions(onDecisions())

	scopes, err := svc.Generate(context.Background(), 20, nil)

	// Then the scope survives, because a description is optional.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 1 || scopes[0].Name != "cli" || scopes[0].Description != "" {
		t.Fatalf("expected the scope without a description, got %v", scopes)
	}
}

func TestScopeService_Generate_ConventionalTypeScopeIsDropped(t *testing.T) {
	// Given a decider that created a scope named after a commit type.
	decider := &stubDecider{decision: &project.ScopeDecision{Confidence: 0.9, Assignments: []project.ScopeAssignment{
		{Dir: "docs", Action: project.ScopeCreate, Scope: "docs"},
		{Dir: "cmd", Action: project.ScopeCreate, Scope: "cli"},
	}}}
	git := &mockGitReader{dirs: []string{"docs", "cmd"}, isGitRepo: true}
	svc := application.NewScopeService(nil, git, decider, nil).WithDecisions(onDecisions())

	scopes, err := svc.Generate(context.Background(), 20, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 1 || scopes[0].Name != "cli" {
		t.Fatalf("expected the commit-type scope to be dropped, got %v", scopes)
	}
}

func TestScopeService_Generate_ShadowModeLetsTheModelDecide(t *testing.T) {
	// Given the layer in shadow mode, which runs the judgment but never lets
	// it decide.
	decider := &stubDecider{decision: &project.ScopeDecision{
		Assignments: []project.ScopeAssignment{{Dir: "cmd", Action: project.ScopeCreate, Scope: "cli"}},
		Confidence:  0.95,
	}}
	llm := &mockLLMClient{scopes: []project.Scope{{Name: "domain", Description: "models"}}}
	git := &mockGitReader{dirs: []string{"cmd", "domain"}, isGitRepo: true}
	var recorded []decision.Observation
	rec := decision.RecorderFunc(func(o decision.Observation) { recorded = append(recorded, o) })
	decisions := application.NewDecisions(decision.ModeShadow, 0.5, 8, rec, nil)
	svc := application.NewScopeService(llm, git, decider, nil).WithDecisions(decisions)

	scopes, err := svc.Generate(context.Background(), 20, nil)

	// Then the model's scope set wins and the disagreement is recorded.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 1 || scopes[0].Name != "domain" {
		t.Fatalf("expected the model scope [domain], got %v", scopes)
	}
	if len(recorded) != 1 {
		t.Fatalf("expected one observation, got %v", recorded)
	}
	o := recorded[0]
	if o.Seam != application.SeamScopeSet || o.Adopted {
		t.Errorf("expected an unadopted scope_set observation, got %+v", o)
	}
	if o.Decision != "cli" || o.Baseline != "domain" || o.Agreement != decision.AgreementDisagree {
		t.Errorf("expected a recorded disagreement between cli and domain, got %+v", o)
	}
}

func TestScopeService_Generate_OffModeNeverCallsTheDecider(t *testing.T) {
	decider := &stubDecider{err: errors.New("must not run")}
	llm := &mockLLMClient{scopes: []project.Scope{{Name: "app"}}}
	git := &mockGitReader{dirs: []string{"application"}, isGitRepo: true}
	svc := application.NewScopeService(llm, git, decider, nil).
		WithDecisions(application.NewDecisions(decision.ModeOff, 0.5, 8, nil, nil))

	if _, err := svc.Generate(context.Background(), 20, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decider.req.Dirs != nil {
		t.Errorf("expected the off mode to skip the judgment, got %+v", decider.req)
	}
}

func TestScopeService_Generate_WeakVerdictFallsBackToTheModel(t *testing.T) {
	// Given a judgment below the confidence floor in the on mode.
	decider := &stubDecider{decision: &project.ScopeDecision{
		Assignments: []project.ScopeAssignment{{Dir: "cmd", Action: project.ScopeCreate, Scope: "cli"}},
		Confidence:  0.4,
	}}
	llm := &mockLLMClient{scopes: []project.Scope{{Name: "domain"}}}
	git := &mockGitReader{dirs: []string{"cmd", "domain"}, isGitRepo: true}
	svc := application.NewScopeService(llm, git, decider, nil).WithDecisions(onDecisions())

	scopes, err := svc.Generate(context.Background(), 20, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 1 || scopes[0].Name != "domain" {
		t.Fatalf("expected the model scope [domain], got %v", scopes)
	}
}

func TestScopeService_Generate_BudgetStopsTheJudgment(t *testing.T) {
	// Given a layer whose per-run budget is already spent.
	decider := &stubDecider{err: errors.New("must not run")}
	llm := &mockLLMClient{scopes: []project.Scope{{Name: "app"}}}
	git := &mockGitReader{dirs: []string{"application"}, isGitRepo: true}
	// A budget of one call is spent by an earlier seam before this service
	// starts, which is how one judgment starves the next.
	decisions := application.NewDecisions(decision.ModeOn, 0.5, 1, nil, nil)
	decisions.Begin().Take("warmup")
	svc := application.NewScopeService(llm, git, decider, nil).WithDecisions(decisions)

	if _, err := svc.Generate(context.Background(), 20, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decider.req.Dirs != nil {
		t.Error("expected a spent budget to skip the judgment")
	}
}
