package openai

import (
	"context"
	"fmt"
	"strings"

	"github.com/gitagenthq/git-agent/application"
)

// rerankSystemPrompt instructs the model to re-order, not invent, candidates.
const rerankSystemPrompt = `You are a senior software engineer performing forensic root-cause analysis.
You are given a symptom (a failing test or build error) and a set of candidate
code changes that preceded it, each identified by a numeric "seq" and the file(s)
it touched. Your job is to re-order the candidates from MOST likely to LEAST
likely to have introduced the symptom, based on whether the change plausibly
causes the described failure.

Rules:
- Re-order ONLY the provided candidates. Do NOT invent new seq numbers.
- Output ONLY a JSON object in this exact format: {"order": [<seq>, <seq>, ...]}
- The order array must contain exactly the seq numbers you were given, each once.
- If you cannot reason about the changes, return the seq numbers in their original order.`

// DiagnoseRerankerAdapter wires application.DiagnoseReranker to the OpenAI
// client. It owns its own Client so the re-rank can target a separate
// (typically more capable, slower) model than commit message generation.
type DiagnoseRerankerAdapter struct {
	client *Client
}

// NewDiagnoseRerankerAdapter builds a re-ranker over the given client. The
// caller constructs the client with the diagnose-specific model/base-url/key
// (see cmd/diagnose.go wiring).
func NewDiagnoseRerankerAdapter(client *Client) *DiagnoseRerankerAdapter {
	return &DiagnoseRerankerAdapter{client: client}
}

// Rerank implements application.DiagnoseReranker. It renders the top-N
// candidates into a prompt, asks the LLM to re-order them by likelihood, and
// returns a []Candidate carrying only the LLM's seq ordering — the
// DiagnoseService maps these back to the original Candidate objects (with their
// scores intact) and enforces the bounded-rerank contract (drop forged seqs,
// backfill any the LLM omitted). So this adapter cannot add, drop, or score
// candidates; it can only propose an order.
func (a *DiagnoseRerankerAdapter) Rerank(ctx context.Context, symptom string, candidates []application.Candidate) ([]application.Candidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	system := rerankSystemPrompt
	user := buildRerankUserPrompt(symptom, candidates)

	raw, err := a.client.RerankDiagnose(ctx, system, user, 2048, rerankMaxTokensCeiling)
	if err != nil {
		return nil, fmt.Errorf("rerank diagnose: %w", err)
	}

	seqs, err := parseRerankOrder(raw)
	if err != nil {
		return nil, fmt.Errorf("rerank diagnose: %w", err)
	}

	// Map the LLM's seq order into bare Candidate values. Only Seq is populated;
	// the service intersects against its allowed set and substitutes the original
	// objects, so the other fields here are intentionally unused.
	out := make([]application.Candidate, 0, len(seqs))
	for _, seq := range seqs {
		out = append(out, application.Candidate{Seq: seq})
	}
	return out, nil
}

// buildRerankUserPrompt renders the symptom + each candidate's seq, tool, files,
// and before/after blob refs. The blob refs let a capable model reason about
// which change is most plausibly causal when the symptom names a file or symbol;
// the model cannot fetch the blobs, but the presence of distinct before/after
// hashes signals a real content change vs. a no-op.
func buildRerankUserPrompt(symptom string, candidates []application.Candidate) string {
	var b strings.Builder
	if strings.TrimSpace(symptom) != "" {
		fmt.Fprintf(&b, "Symptom:\n%s\n\n", symptom)
	} else {
		b.WriteString("Symptom: (none provided)\n\n")
	}
	b.WriteString("Candidates (in deterministic order):\n")
	for _, c := range candidates {
		files := strings.Join(c.Files, ", ")
		if files == "" {
			files = "(none)"
		}
		fmt.Fprintf(&b, "- seq %d | tool %s | files %s | %s -> %s\n",
			c.Seq, c.Tool, files, c.BeforeBlob, c.AfterBlob)
	}
	b.WriteString("\nReturn the seq numbers in order from most to least likely to have caused the symptom, as {\"order\": [...]}.\n")
	return b.String()
}

// rerankResponse is the JSON shape the LLM must return.
type rerankResponse struct {
	Order []int64 `json:"order"`
}

// parseRerankOrder extracts the ordered seq list from the LLM's raw response,
// tolerating surrounding prose by falling back to extractJSON.
func parseRerankOrder(raw string) ([]int64, error) {
	var resp rerankResponse
	if err := unmarshalLLMJSON(raw, "", &resp); err != nil {
		return nil, fmt.Errorf("parse order: %w", err)
	}
	if len(resp.Order) == 0 {
		// Distinguish "empty" from "malformed" for the caller's retry path.
		return nil, fmt.Errorf("parse order: empty or missing \"order\" array\nraw: %s", extractJSON(raw))
	}
	return resp.Order, nil
}

// Ensure the adapter satisfies the port at compile time.
var _ application.DiagnoseReranker = (*DiagnoseRerankerAdapter)(nil)
