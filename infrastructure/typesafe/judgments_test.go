package typesafe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/commit"
	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
	"github.com/gitagenthq/git-agent/infrastructure/typesafe"
)

// answeringServer answers every question id in answers and records the request.
func answeringServer(t *testing.T, answers map[string]any) (*typesafe.Client, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, captured); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-1.13.0",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 42, "output_tokens": 7},
		})
	})
	return client, captured
}

func noulAnswer(v float64) map[string]any {
	return map[string]any{"type": "noul", "noul": v}
}

func choiceAnswer(choice string, confidence float64, probs map[string]float64) map[string]any {
	return map[string]any{"type": "choice", "choice": choice, "confidence": confidence, "probabilities": probs}
}

func TestTechClassifier_AsksEveryCandidateInOneRequest(t *testing.T) {
	// Given a project with Go evidence.
	client, captured := answeringServer(t, map[string]any{
		"tech_go":      noulAnswer(0.97),
		"tech_python":  noulAnswer(0.02),
		"tech_docker":  noulAnswer(0.01),
		"tech_mac":     noulAnswer(0.01),
		"tech_javac":   noulAnswer(0.03),
		"tech_makefil": noulAnswer(0.01),
	})
	// Every other identifier is absent from the answer, which must be read as
	// "no evidence" rather than as an error.
	verdict, err := typesafe.NewTechClassifier(client).ClassifyTechnologies(context.Background(),
		domainGitignore.ClassifyRequest{
			OS: "linux", Dirs: []string{"application", "cmd"},
			Files: []string{"go.mod", "main.go", "application/service.go"},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(captured.Questions) != len(domainGitignore.CandidateTechnologies) {
		t.Errorf("expected one question per candidate identifier, got %d of %d",
			len(captured.Questions), len(domainGitignore.CandidateTechnologies))
	}
	if !containsString(verdict.Technologies, "go") {
		t.Errorf("expected go in the list, got %v", verdict.Technologies)
	}
	for _, tech := range verdict.Technologies {
		if tech == "python" || tech == "docker" {
			t.Errorf("expected %q to be excluded below the presence threshold, got %v", tech, verdict.Technologies)
		}
	}
	if verdict.Confidence < 0.9 {
		t.Errorf("expected the verdict to carry the weakest presence probability, got %v", verdict.Confidence)
	}
}

func TestTechClassifier_AddsTheOperatingSystemWithoutLoweringConfidence(t *testing.T) {
	client, _ := answeringServer(t, map[string]any{"tech_go": noulAnswer(0.9)})

	verdict, err := typesafe.NewTechClassifier(client).ClassifyTechnologies(context.Background(),
		domainGitignore.ClassifyRequest{OS: "darwin", Files: []string{"main.go"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Technologies[0] != "macos" {
		t.Errorf("expected the platform identifier first, got %v", verdict.Technologies)
	}
	if verdict.Confidence != 0.9 {
		t.Errorf("expected the confidence to come from the model answers, got %v", verdict.Confidence)
	}
}

func TestTechClassifier_RefusesAnEmptyVerdict(t *testing.T) {
	client, _ := answeringServer(t, map[string]any{})

	if _, err := typesafe.NewTechClassifier(client).ClassifyTechnologies(context.Background(),
		domainGitignore.ClassifyRequest{OS: "", Files: []string{"notes.txt"}}); err == nil {
		t.Fatal("expected an error when no technology is found")
	}
}

func TestCandidateTechnologies_HaveUniqueIdentifiers(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range domainGitignore.CandidateTechnologies {
		if c.ID == "" {
			t.Errorf("candidate %+v has no identifier", c)
		}
		if seen[c.ID] {
			t.Errorf("duplicate candidate identifier %q: a Choice criteria map would silently drop it", c.ID)
		}
		if c.Evidence == "" {
			t.Errorf("candidate %q has no evidence description", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestGroupDecider_MergesBucketsByVote(t *testing.T) {
	// Given two buckets that each name the same pair as one commit.
	client, captured := answeringServer(t, map[string]any{
		"bucket_a": choiceAnswer("a,b", 0.8, map[string]float64{"a": 0.1, "a,b": 0.8, "b": 0.1}),
		"bucket_b": choiceAnswer("b,a", 0.8, map[string]float64{"b": 0.1, "b,a": 0.8, "a": 0.1}),
	})
	buckets := twoBuckets()

	verdict, err := typesafe.NewGroupDecider(client).DecideGroups(context.Background(),
		commit.GroupDecisionRequest{Buckets: buckets})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	groups, problems := verdict.DecidedGroups(commit.GroupDecisionRequest{Buckets: buckets})
	if len(problems) != 0 {
		t.Errorf("expected a complete decision, got problems %v", problems)
	}
	if len(groups) != 1 || len(groups[0]) != 2 {
		t.Errorf("expected one group holding both files, got %v", groups)
	}
	if len(captured.Questions) != 2 {
		t.Errorf("expected one question per bucket, got %d", len(captured.Questions))
	}
	if verdict.Confidence < 0.7 {
		t.Errorf("expected the mean probability of the chosen options, got %v", verdict.Confidence)
	}
}

func TestGroupDecider_KeepsSingleBucketsApart(t *testing.T) {
	client, _ := answeringServer(t, map[string]any{
		"bucket_a": choiceAnswer("a", 0.9, map[string]float64{"a": 0.9, "a,b": 0.1}),
		"bucket_b": choiceAnswer("b", 0.9, map[string]float64{"b": 0.9, "a,b": 0.1}),
	})
	buckets := twoBuckets()

	verdict, err := typesafe.NewGroupDecider(client).DecideGroups(context.Background(),
		commit.GroupDecisionRequest{Buckets: buckets})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	groups, _ := verdict.DecidedGroups(commit.GroupDecisionRequest{Buckets: buckets})
	if len(groups) != 2 {
		t.Errorf("expected two single-bucket groups, got %v", groups)
	}
}

func TestGroupDecider_SingleBucketNeedsNoRequest(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected no request for a single bucket")
	})
	buckets := twoBuckets()[:1]

	verdict, err := typesafe.NewGroupDecider(client).DecideGroups(context.Background(),
		commit.GroupDecisionRequest{Buckets: buckets})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(verdict.GroupBuckets) != 1 {
		t.Errorf("expected the bucket to stand alone, got %v", verdict.GroupBuckets)
	}
}

func TestGroupDecider_RefusesMoreBucketsThanOneRequestCarries(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected no request for an oversized bucket set")
	})
	var buckets []commit.FileBucket
	for i := 0; i < 40; i++ {
		buckets = append(buckets, commit.FileBucket{ID: string(rune('a' + i%26)), Files: []string{"x.go"}})
	}

	if _, err := typesafe.NewGroupDecider(client).DecideGroups(context.Background(),
		commit.GroupDecisionRequest{Buckets: buckets}); err == nil {
		t.Fatal("expected an error above the bucket limit")
	}
}

func TestGroupDecision_RejectsAnIncompleteOrRepeatedAnswer(t *testing.T) {
	buckets := twoBuckets()

	_, problems := (&commit.GroupDecision{GroupBuckets: [][]string{{"a", "c"}}}).DecidedGroups(
		commit.GroupDecisionRequest{Buckets: buckets})
	if len(problems) != 2 {
		t.Errorf("expected problems for the unknown and the undecided bucket, got %v", problems)
	}

	_, problems = (&commit.GroupDecision{GroupBuckets: [][]string{{"a"}, {"a", "b"}}}).DecidedGroups(
		commit.GroupDecisionRequest{Buckets: buckets})
	if len(problems) != 1 || !strings.Contains(problems[0], "duplicate") {
		t.Errorf("expected a duplicate bucket problem, got %v", problems)
	}
}

func TestFailureRouter_ClassifiesTheRejectedMessage(t *testing.T) {
	client, captured := answeringServer(t, map[string]any{
		"action": choiceAnswer("rewrite", 0.91, map[string]float64{"rewrite": 0.91, "replan": 0.08, "give_up": 0.01}),
	})

	route, err := typesafe.NewFailureRouter(client).RouteFailure(context.Background(), commit.FailureRouteRequest{
		HookReason: "title exceeds 50 characters", Title: "feat(app): a very long title",
		Files: []string{"application/a.go"}, RemainingFiles: []string{"cmd/c.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route.Action != commit.FailureRewrite {
		t.Errorf("expected a rewrite route, got %s", route.Action)
	}
	if route.Confidence < 0.9 {
		t.Errorf("expected the confidence to be carried, got %v", route.Confidence)
	}
	if typesafe.NewFailureRouter(client).HookRouteFloor() <= 0 {
		t.Error("expected the router to publish a confidence floor")
	}
	state := captured.State["purpose"]
	if state == nil {
		t.Errorf("expected a structured state, got %v", captured.State)
	}
}

func TestFailureRouter_RejectsAnUnknownAction(t *testing.T) {
	client, _ := answeringServer(t, map[string]any{
		"action": choiceAnswer("try_something_else", 0.9, nil),
	})

	if _, err := typesafe.NewFailureRouter(client).RouteFailure(context.Background(),
		commit.FailureRouteRequest{HookReason: "x"}); err == nil {
		t.Fatal("expected an error for an action code never offered")
	}
}

func TestTypeScopeJudge_ReadsTheWeakerOfTheTwoConfidences(t *testing.T) {
	client, captured := answeringServer(t, map[string]any{
		"type":  choiceAnswer("fix", 0.96, map[string]float64{"fix": 0.96, "feat": 0.04}),
		"scope": choiceAnswer("app", 0.62, map[string]float64{"app": 0.62, "cli": 0.38}),
	})

	verdict, err := typesafe.NewTypeScopeJudge(client).JudgeTypeScope(context.Background(), commit.TypeScopeRequest{
		Files:  []commit.FileChange{{Path: "application/a.go", Adds: 4, Dels: 1}},
		Scopes: map[string]string{"app": "services in application/", "cli": "commands in cmd/"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Type != "fix" || verdict.Scope != "app" {
		t.Errorf("expected fix(app), got %s(%s)", verdict.Type, verdict.Scope)
	}
	if verdict.ScopeConfidence != 0.62 || verdict.TypeConfidence != 0.96 {
		t.Errorf("expected both confidences to be carried separately, got type %v scope %v",
			verdict.TypeConfidence, verdict.ScopeConfidence)
	}
	if verdict.Confidence() != 0.62 {
		t.Errorf("expected the combined confidence to be the weaker part, got %v", verdict.Confidence())
	}
	if verdict.Probabilities["scope:app"] != 0.62 {
		t.Errorf("expected the raw distribution to be carried, got %v", verdict.Probabilities)
	}
	rendered := renderState(captured.State)
	if strings.Contains(rendered, "func main") {
		t.Errorf("expected no diff body in the judge state, got %q", rendered)
	}
}

func TestTypeScopeVerdict_PinsOnlyTheScope(t *testing.T) {
	verdict := commit.TypeScopeVerdict{Type: "chore", Scope: "app", TypeConfidence: 0.99, ScopeConfidence: 0.95}

	if got := verdict.PinnedScope(true); got != "app" {
		t.Errorf("expected the scope to be pinned, got %q", got)
	}
	if got := verdict.PinnedScope(false); got != "" {
		t.Errorf("expected a refused scope to be empty, got %q", got)
	}
}

func TestTypeScopeJudge_RefusesAnUnofferedScope(t *testing.T) {
	client, _ := answeringServer(t, map[string]any{
		"type":  choiceAnswer("fix", 0.9, nil),
		"scope": choiceAnswer("infrastructure", 0.9, nil),
	})

	if _, err := typesafe.NewTypeScopeJudge(client).JudgeTypeScope(context.Background(), commit.TypeScopeRequest{
		Files: []commit.FileChange{{Path: "a.go"}}, Scopes: map[string]string{"app": "services"},
	}); err == nil {
		t.Fatal("expected an error for a scope code never offered")
	}
}

func TestTypeScopeJudge_RefusesWithoutScopes(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected no request without a scope to choose from")
	})
	if _, err := typesafe.NewTypeScopeJudge(client).JudgeTypeScope(context.Background(),
		commit.TypeScopeRequest{Files: []commit.FileChange{{Path: "a.go"}}}); err == nil {
		t.Fatal("expected an error without any scope")
	}
}

func TestJudgeSupportsLanguage(t *testing.T) {
	cases := map[string]bool{
		"":        true,
		"en":      true,
		"en-US":   true,
		"EN_gb":   false,
		"zh-CN":   false,
		"ja":      false,
		" python": false,
	}
	for lang, want := range cases {
		if got := commit.JudgeSupportsLanguage(lang); got != want {
			t.Errorf("JudgeSupportsLanguage(%q) = %v, want %v", lang, got, want)
		}
	}
}

func twoBuckets() []commit.FileBucket {
	return []commit.FileBucket{
		{ID: "a", Label: "application", Files: []string{"application/a.go"}, Adds: 4, Dels: 1},
		{ID: "b", Label: "cmd", Files: []string{"cmd/c.go"}, Adds: 3, Dels: 0},
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func renderState(state map[string]any) string {
	encoded, _ := json.Marshal(state)
	return string(encoded)
}
