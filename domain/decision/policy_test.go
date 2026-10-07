package decision_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/decision"
)

func TestParseMode(t *testing.T) {
	cases := map[string]decision.Mode{
		"":         decision.DefaultMode,
		"shadow":   decision.ModeShadow,
		"ON":       decision.ModeOn,
		" on ":     decision.ModeOn,
		"off":      decision.ModeOff,
		"nonsense": decision.DefaultMode,
	}
	for in, want := range cases {
		if got := decision.ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPolicy_ShadowRunsButNeverDecides(t *testing.T) {
	p := decision.NewPolicy(decision.ModeShadow, 0.5, 4)

	if !p.Enabled() {
		t.Fatal("expected shadow mode to run the judgment")
	}
	if p.Adopt(0.99) {
		t.Fatal("expected shadow mode never to decide")
	}
}

func TestPolicy_OnAdoptsAboveTheFloor(t *testing.T) {
	p := decision.NewPolicy(decision.ModeOn, 0.7, 4)

	if !p.Adopt(0.7) {
		t.Error("expected a confidence at the floor to be adopted")
	}
	if p.Adopt(0.69) {
		t.Error("expected a confidence below the floor to be refused")
	}
}

func TestPolicy_AdoptAtUsesTheStricterFloor(t *testing.T) {
	p := decision.NewPolicy(decision.ModeOn, 0.5, 4)

	if p.AdoptAt(0.6, 0.9) {
		t.Error("expected the stricter per-seam floor to win")
	}
	if !p.AdoptAt(0.95, 0.9) {
		t.Error("expected a confidence above both floors to be adopted")
	}
}

func TestPolicy_OffRunsNothing(t *testing.T) {
	p := decision.NewPolicy(decision.ModeOff, 0.1, 4)

	if p.Enabled() {
		t.Error("expected the off mode to be disabled")
	}
	if p.Take() {
		t.Error("expected the off mode to reserve no call")
	}
	if p.Adopt(1) {
		t.Error("expected the off mode never to decide")
	}
}

func TestPolicy_BudgetIsFinite(t *testing.T) {
	p := decision.NewPolicy(decision.ModeShadow, 0.5, 2)

	for i := 0; i < 2; i++ {
		if !p.Take() {
			t.Fatalf("call %d: expected the budget to allow the judgment", i+1)
		}
	}
	if p.Take() {
		t.Error("expected the third call to exceed the budget")
	}
	if !p.Spent() {
		t.Error("expected the policy to report a spent budget")
	}
}

func TestPolicy_CopiesShareTheSpentCount(t *testing.T) {
	p := decision.NewPolicy(decision.ModeOn, 0.5, 1)
	p.Take()
	if p.Used() != 1 {
		t.Fatalf("expected one reserved call, got %d", p.Used())
	}
}

func TestNewPolicy_RepairsOutOfRangeValues(t *testing.T) {
	p := decision.NewPolicy(decision.ModeOn, 5, -3)

	if p.MinConfidence != decision.DefaultMinConfidence {
		t.Errorf("expected the default floor, got %v", p.MinConfidence)
	}
	// A non-positive budget falls back to the default, which still allows a
	// first call.
	if !p.Take() {
		t.Error("expected a non-positive budget to fall back to the default")
	}
}

func TestNewRecorder_WritesOneLinePerObservation(t *testing.T) {
	var buf bytes.Buffer
	rec := decision.NewRecorder(&buf)

	rec.Record(decision.Observation{
		Seam: "type_scope", Mode: decision.ModeShadow, Decision: "fix(app)",
		Baseline: "feat(app)", Agreement: decision.AgreementDisagree,
		Confidence: 0.62, Latency: 1500 * 1000 * 1000, InputTokens: 812,
	})

	line := buf.String()
	for _, want := range []string{"type_scope", "shadow", "confidence=62", "decided=fix(app)", "baseline=feat(app)", "agreement=disagree", "in_tokens=812"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the line to contain %q, got %q", want, line)
		}
	}
}

func TestNewRecorder_NilWriterDiscards(t *testing.T) {
	// A nil writer must not panic.
	decision.NewRecorder(nil).Record(decision.Observation{Seam: "scope_set", Mode: decision.ModeOn})
}

func TestMultiRecorder_SkipsNilMembers(t *testing.T) {
	var seen int
	multi := decision.MultiRecorder{nil, decision.RecorderFunc(func(decision.Observation) { seen++ })}

	multi.Record(decision.Observation{Seam: "grouping"})
	if seen != 1 {
		t.Errorf("expected the non-nil recorder to receive the observation, got %d", seen)
	}
}
