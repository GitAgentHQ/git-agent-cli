package typesafe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/domain/project"
	"github.com/gitagenthq/git-agent/infrastructure/typesafe"
)

// capturedRequest holds what the decider actually sent.
type capturedRequest struct {
	State     map[string]any               `json:"state"`
	Questions map[string]typesafe.Question `json:"questions"`
}

// newTestServer serves one request handler and returns a client pointed at it.
func newTestServer(t *testing.T, handler http.HandlerFunc) *typesafe.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return typesafe.NewClient("test-key", srv.URL, "jev-latest", 5*time.Second)
}

// newCapturingClient returns a decider plus the request the next call sends. The
// handler answers every question id found in choices.
func newCapturingClient(t *testing.T, choices map[string]string) (*typesafe.ScopeDecider, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, captured); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		answers := make(map[string]any, len(choices))
		for id, choice := range choices {
			answers[id] = map[string]any{
				"type":          "choice",
				"choice":        choice,
				"probabilities": map[string]float64{choice: 0.9},
				"confidence":    0.8,
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-1.13.0",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 100, "output_tokens": 10},
		})
	})
	return typesafe.NewScopeDecider(client), captured
}

func TestScopeDecider_JudgesEveryCandidateDirectoryOnce(t *testing.T) {
	decider, captured := newCapturingClient(t, map[string]string{
		"dir_application":  "new",
		"dir_cmd":          "cli",
		"dir_node_modules": "skip",
	})

	_, err := decider.DecideScopes(context.Background(), project.ScopeDecisionRequest{
		Dirs:           []string{"application", "cmd", "node_modules", ".github"},
		ExistingScopes: []project.Scope{{Name: "cli", Description: "command surface in cmd/"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(captured.Questions) != 2 {
		t.Fatalf("expected one question per candidate directory, got %v", keys(captured.Questions))
	}
	if _, ok := captured.Questions["dir_application"]; !ok {
		t.Errorf("expected application to be judged, got %v", keys(captured.Questions))
	}
	criteria := captured.Questions["dir_cmd"].Criteria.(map[string]any)
	for _, option := range []string{"cli", "new", "skip"} {
		if _, ok := criteria[option]; !ok {
			t.Errorf("expected option %q in the criteria, got %v", option, keysAny(criteria))
		}
	}
}

func TestScopeDecider_MapsChoicesToActions(t *testing.T) {
	decider, _ := newCapturingClient(t, map[string]string{
		"dir_application":    "new",
		"dir_infrastructure": "infra",
		"dir_scripts":        "skip",
	})

	decision, err := decider.DecideScopes(context.Background(), project.ScopeDecisionRequest{
		Dirs:           []string{"application", "infrastructure", "scripts"},
		ExistingScopes: []project.Scope{{Name: "infra", Description: "adapters"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]struct {
		action project.ScopeAction
		scope  string
	}{
		"application":    {project.ScopeCreate, "app"},
		"infrastructure": {project.ScopeReuse, "infra"},
		"scripts":        {project.ScopeSkip, ""},
	}
	if len(decision.Assignments) != len(want) {
		t.Fatalf("expected one assignment per directory, got %+v", decision.Assignments)
	}
	for _, a := range decision.Assignments {
		expected := want[a.Dir]
		if a.Action != expected.action || a.Scope != expected.scope {
			t.Errorf("directory %q: expected %s/%q, got %s/%q", a.Dir, expected.action, expected.scope, a.Action, a.Scope)
		}
	}

	created := decision.Created()
	if len(created) != 1 || created[0].Name != "app" || created[0].Dir != "application" {
		t.Errorf("expected one proposed scope for application, got %v", created)
	}
}

func TestScopeDecider_RejectsChoiceOutsideTheCandidateSet(t *testing.T) {
	// Given a model that answers with a scope name code never offered.
	decider, _ := newCapturingClient(t, map[string]string{"dir_cmd": "invented"})

	decision, err := decider.DecideScopes(context.Background(), project.ScopeDecisionRequest{
		Dirs:           []string{"cmd"},
		ExistingScopes: []project.Scope{{Name: "cli", Description: "command surface"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Then the answer is dropped instead of adding a scope nobody proposed.
	if len(decision.Assignments) != 0 {
		t.Fatalf("expected no assignment for an unoffered option, got %+v", decision.Assignments)
	}
}

func TestScopeDecider_IgnoresMissingAnswer(t *testing.T) {
	decider, _ := newCapturingClient(t, map[string]string{})

	decision, err := decider.DecideScopes(context.Background(), project.ScopeDecisionRequest{Dirs: []string{"cmd"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Assignments) != 0 {
		t.Fatalf("expected an undecided directory to add no scope, got %+v", decision.Assignments)
	}
}

func TestScopeDecider_SkipsRequestWhenNoCandidateRemains(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected no request when every directory is filtered out")
	})

	decision, err := typesafe.NewScopeDecider(client).DecideScopes(context.Background(),
		project.ScopeDecisionRequest{Dirs: []string{"node_modules", ".git", "dist"}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Assignments) != 0 {
		t.Fatalf("expected no assignment, got %+v", decision.Assignments)
	}
}

func keys(m map[string]typesafe.Question) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysAny(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
