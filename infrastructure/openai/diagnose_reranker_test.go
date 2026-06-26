package openai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
)

// newOfflineReranker builds a DiagnoseRerankerAdapter whose underlying Client
// never touches the network: the call seam is swapped to return canned raw
// responses, exercised by the fn callback.
func newOfflineReranker(t *testing.T, raw string) (*DiagnoseRerankerAdapter, *int) {
	t.Helper()
	c := &Client{}
	calls := 0
	c.call = func(ctx context.Context, system, user string, maxTokens, ceiling int) (string, error) {
		calls++
		return raw, nil
	}
	return NewDiagnoseRerankerAdapter(c), &calls
}

func sampleCandidates() []application.Candidate {
	return []application.Candidate{
		{Seq: 10, Tool: "Edit", Files: []string{"a.go"}, BeforeBlob: "b1", AfterBlob: "a1", Score: 0.7},
		{Seq: 12, Tool: "Edit", Files: []string{"b.go"}, BeforeBlob: "b2", AfterBlob: "a2", Score: 0.5},
		{Seq: 15, Tool: "Write", Files: []string{"c.go"}, BeforeBlob: "", AfterBlob: "a3", Score: 0.3},
	}
}

func TestDiagnoseRerankerAdapter_RerankValidOrder(t *testing.T) {
	a, calls := newOfflineReranker(t, `{"order": [12, 10, 15]}`)
	out, err := a.Rerank(context.Background(), "test X fails", sampleCandidates())
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 3 || out[0].Seq != 12 || out[1].Seq != 10 || out[2].Seq != 15 {
		t.Fatalf("got %+v, want seq order [12 10 15]", seqsOf(out))
	}
	if *calls != 1 {
		t.Fatalf("expected 1 LLM call, got %d", *calls)
	}
}

func TestDiagnoseRerankerAdapter_RerankReordersOnlyBySeq(t *testing.T) {
	// The adapter returns only Seq; scores and other fields stay zero here.
	// DiagnoseService intersects against its original objects, so scores there
	// are preserved. Verify the adapter does not synthesize scores.
	a, _ := newOfflineReranker(t, `{"order": [15, 10, 12]}`)
	out, _ := a.Rerank(context.Background(), "symptom", sampleCandidates())
	for _, c := range out {
		if c.Score != 0 || c.Tool != "" || c.BeforeBlob != "" {
			t.Fatalf("adapter must return bare candidates (seq only), got %+v", c)
		}
	}
}

func TestDiagnoseRerankerAdapter_RerankMalformedJSON(t *testing.T) {
	a, _ := newOfflineReranker(t, `not json at all`)
	if _, err := a.Rerank(context.Background(), "symptom", sampleCandidates()); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestDiagnoseRerankerAdapter_RerankEmptyOrderArray(t *testing.T) {
	a, _ := newOfflineReranker(t, `{"order": []}`)
	if _, err := a.Rerank(context.Background(), "symptom", sampleCandidates()); err == nil {
		t.Fatal("expected error on empty order array")
	}
}

func TestDiagnoseRerankerAdapter_RerankLLMError(t *testing.T) {
	c := &Client{}
	c.call = func(ctx context.Context, system, user string, maxTokens, ceiling int) (string, error) {
		return "", errors.New("boom")
	}
	a := NewDiagnoseRerankerAdapter(c)
	if _, err := a.Rerank(context.Background(), "symptom", sampleCandidates()); err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

func TestDiagnoseRerankerAdapter_RerankEmptyCandidatesNoCall(t *testing.T) {
	a, calls := newOfflineReranker(t, `{"order": [1]}`)
	out, err := a.Rerank(context.Background(), "symptom", nil)
	if err != nil {
		t.Fatalf("Rerank on empty input: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected no candidates, got %d", len(out))
	}
	if *calls != 0 {
		t.Fatalf("LLM must not be called for empty input, got %d calls", *calls)
	}
}

func TestBuildRerankUserPrompt(t *testing.T) {
	prompt := buildRerankUserPrompt("capture panics on empty diff", sampleCandidates())
	for _, want := range []string{"capture panics on empty diff", "seq 10", "seq 12", "seq 15", "a.go", "b.go", "c.go", "b1", "a1"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
}

// seqsOf returns the seq numbers from a candidate slice, for assertions.
func seqsOf(cs []application.Candidate) []int64 {
	out := make([]int64, len(cs))
	for i, c := range cs {
		out[i] = c.Seq
	}
	return out
}
