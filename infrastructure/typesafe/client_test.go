package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"

	"github.com/gitagenthq/git-agent/infrastructure/typesafe"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *typesafe.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	// Retry delays are asserted through the elapsed time, so the suite stays
	// fast by serving every answer immediately.
	return typesafe.NewClient("test-key", srv.URL, "jev-latest", 5*time.Second)
}

func answerBody(choice string) string {
	return `{"model":"jev-1.13.0","answers":{"pick":{"type":"choice","choice":"` + choice +
		`","probabilities":{"` + choice + `":0.8,"other":0.2},"confidence":0.7}},"usage":{"input_tokens":10,"output_tokens":3}}`
}

func TestClient_Evaluate_ParsesTypedAnswers(t *testing.T) {
	var gotPath, gotAuth string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Write([]byte(answerBody("cli")))
	})

	result, err := client.Evaluate(context.Background(), "state text",
		map[string]typesafe.Question{"pick": typesafe.Choice("Which scope?", map[string]string{"cli": "the cli", "other": "the rest"})})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/v1/systemone" {
		t.Errorf("expected the System One endpoint, got %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("expected a bearer token, got %q", gotAuth)
	}
	answer, ok := result.Answer("pick")
	if !ok || answer.Choice == nil || *answer.Choice != "cli" {
		t.Fatalf("expected choice cli, got %+v", answer)
	}
	if answer.Confidence == nil || *answer.Confidence < 0.6 {
		t.Errorf("expected the answer to carry confidence, got %+v", answer.Confidence)
	}
	if got := answer.Probability("other"); got != 0.2 {
		t.Errorf("expected the full distribution, got %v", answer.Probabilities)
	}
	if result.Usage.InputTokens != 10 {
		t.Errorf("expected usage to be reported, got %+v", result.Usage)
	}
}

func TestClient_Evaluate_SendsStateAndQuestions(t *testing.T) {
	var payload struct {
		Model     string                       `json:"model"`
		State     map[string]any               `json:"state"`
		Questions map[string]typesafe.Question `json:"questions"`
	}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		w.Write([]byte(`{"answers":{"is_new":{"type":"noul","noul":0.25}}}`))
	})

	_, err := client.Evaluate(context.Background(),
		map[string]any{"directories": []string{"cmd"}},
		map[string]typesafe.Question{
			"is_new": typesafe.Noul("Is this new?", &typesafe.NoulCriteria{Yes: "No prior scope", No: "A scope exists"}),
			"mix":    typesafe.Score("How much does this mix unrelated changes?", []string{"One change.", "Several unrelated changes."}),
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload.Model != "jev-latest" {
		t.Errorf("expected the configured model, got %q", payload.Model)
	}
	if payload.Questions["is_new"].Type != typesafe.QuestionNoul {
		t.Errorf("expected a noul question, got %+v", payload.Questions["is_new"])
	}
	if criteria, ok := payload.Questions["is_new"].Criteria.(map[string]any); !ok || criteria["true"] != "No prior scope" {
		t.Errorf("expected noul criteria to state what yes means, got %+v", payload.Questions["is_new"].Criteria)
	}
	levels, ok := payload.Questions["mix"].Criteria.([]any)
	if !ok || len(levels) != 2 || levels[0] != "One change." {
		t.Errorf("expected an ordered rubric for the score question, got %+v", payload.Questions["mix"].Criteria)
	}
	if _, ok := payload.State["directories"]; !ok {
		t.Errorf("expected the structured state to be sent, got %+v", payload.State)
	}
}

func TestClient_Evaluate_RetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(answerBody("cli")))
	})

	result, err := client.Evaluate(context.Background(), "state",
		map[string]typesafe.Question{"pick": typesafe.Choice("Which?", map[string]string{"cli": "a", "other": "b"})})
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if result.Model == "" {
		t.Error("expected a decoded result")
	}
	if calls.Load() != 2 {
		t.Errorf("expected exactly one retry, got %d calls", calls.Load())
	}
}

func TestClient_Evaluate_DoesNotRetryUnauthorized(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`))
	})

	_, err := client.Evaluate(context.Background(), "state",
		map[string]typesafe.Question{"pick": typesafe.Choice("Which?", map[string]string{"a": "a"})})

	var apiErr *agentErrors.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an API error, got %v", err)
	}
	if apiErr.HTTPStatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 to be reported, got %d", apiErr.HTTPStatusCode)
	}
	if !strings.Contains(apiErr.Message, "TYPESAFE_API_KEY") {
		t.Errorf("expected the message to name the fix, got %q", apiErr.Message)
	}
	if calls.Load() != 1 {
		t.Errorf("expected no retry on an auth failure, got %d calls", calls.Load())
	}
}

func TestClient_Evaluate_ReportsUnprocessableRequest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":"questions.pick.criteria must be a map"}`))
	})

	_, err := client.Evaluate(context.Background(), "state",
		map[string]typesafe.Question{"pick": typesafe.Choice("Which?", map[string]string{"a": "a"})})
	if err == nil || !strings.Contains(err.Error(), "criteria must be a map") {
		t.Fatalf("expected the validation detail to reach the caller, got %v", err)
	}
}

func TestClient_Evaluate_StopsOnCancelledContext(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(answerBody("cli")))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Evaluate(ctx, "state",
		map[string]typesafe.Question{"pick": typesafe.Choice("Which?", map[string]string{"a": "a"})})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancelled context to be reported, got %v", err)
	}
}

func TestClient_Evaluate_RefusesOversizedStateBeforeSending(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(answerBody("a")))
	})

	_, err := client.Evaluate(context.Background(), strings.Repeat("x", 400_000),
		map[string]typesafe.Question{"pick": typesafe.Choice("Which?", map[string]string{"a": "a"})})

	var apiErr *agentErrors.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an API error, got %v", err)
	}
	if !strings.Contains(apiErr.Message, "Jev state too large") {
		t.Errorf("expected the message to name the limit, got %q", apiErr.Message)
	}
	if calls.Load() != 0 {
		t.Errorf("expected the request to be refused before the wire, got %d calls", calls.Load())
	}
}

func TestClient_Evaluate_RequiresQuestions(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected no request for an empty question set")
	})
	if _, err := client.Evaluate(context.Background(), "state", nil); err == nil {
		t.Fatal("expected an error for an empty question set")
	}
}
