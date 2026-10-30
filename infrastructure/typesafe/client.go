// Package typesafe calls the TypeSafe System One API (Jev).
//
// A System One model answers typed questions about a state and returns typed
// answers. It does not write text. This client therefore owns decisions:
// selection, classification, and presence. Wording stays with the generative
// provider in infrastructure/openai.
//
// The API has one endpoint, POST /v1/systemone. It needs no SDK in Go, so the
// client is built on net/http and encoding/json to keep the release builds
// cgo-free.
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

// DefaultBaseURL is the TypeSafe API root.
const DefaultBaseURL = "https://api.typesafe.ai"

// DefaultModel is the model alias used when no model is configured. The alias
// moves when a new release ships; a versioned ID pins the behavior instead.
const DefaultModel = "jev-latest"

// Transport-level bounds applied when the caller passes a non-positive value.
const (
	defaultRequestTimeout = 30 * time.Second
	maxAttempts           = 3
	retryBaseDelay        = 500 * time.Millisecond

	// maxStateTokens is the preflight ceiling on the estimated size of one
	// state. Jev ingests the state once and evaluates every question against
	// it; the documented budget is 32k tokens for the state plus the single
	// longest question, and 64k for the whole request. The byte-per-4 estimate
	// is conservative for code and English text, so refusing at 30k keeps a
	// request inside the documented budget. Jev's own 422 rejection catches
	// the cases the estimate undershoots.
	maxStateTokens = 30_000
)

// Question kinds accepted by the API.
const (
	QuestionNoul   = "noul"
	QuestionChoice = "choice"
	QuestionScore  = "score"
)

// Question is one typed question. Instructions and Criteria accept a string, an
// object, or an array, so they are typed as any to pass the structure straight
// through. Criteria holds an object for Noul and Choice and an ordered array
// for Score.
type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// NoulCriteria states what a yes and a no mean for a Noul question.
type NoulCriteria struct {
	Yes string
	No  string
}

// Noul builds a yes/no question.
func Noul(instructions any, criteria *NoulCriteria) Question {
	q := Question{Type: QuestionNoul, Instructions: instructions}
	if criteria != nil {
		q.Criteria = map[string]any{"true": criteria.Yes, "false": criteria.No}
	}
	return q
}

// Choice builds a selection question over a fixed option set. Use it when code
// holds every legal answer, so the model selects and never invents.
func Choice(instructions any, options map[string]string) Question {
	criteria := make(map[string]any, len(options))
	for option, description := range options {
		if description == "" {
			criteria[option] = nil
			continue
		}
		criteria[option] = description
	}
	return Question{Type: QuestionChoice, Instructions: instructions, Criteria: criteria}
}

// Score builds an ordered-rubric question. Each level must stand on its own.
func Score(instructions any, levels []string) Question {
	return Question{Type: QuestionScore, Instructions: instructions, Criteria: levels}
}

// Answer is one typed result. Noul answers carry Noul only; Choice and Score
// answers carry Confidence, derived from the answer's distribution.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        *string            `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Probability returns the probability the answer assigned to one option. A
// missing option or a non-distribution answer returns 0.
func (a Answer) Probability(option string) float64 {
	if a.Probabilities == nil {
		return 0
	}
	return a.Probabilities[option]
}

// Usage reports the token cost of one request.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Result holds one answer per question, keyed by the ids the caller chose.
type Result struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Answer returns the answer for one question id. The second result reports
// whether the question was part of the request.
func (r *Result) Answer(id string) (Answer, bool) {
	if r == nil {
		return Answer{}, false
	}
	a, ok := r.Answers[id]
	return a, ok
}

// Client evaluates questions against a state through the System One endpoint.
type Client struct {
	httpClient     *http.Client
	apiKey         string
	baseURL        string
	model          string
	maxAttempts    int
	maxStateTokens int
}

// NewClient builds a client. Empty apiKey, baseURL, or model fall back to the
// defaults for baseURL and model; an empty key produces unauthorized answers at
// request time, which the caller reports as a configuration error.
func NewClient(apiKey, baseURL, model string, requestTimeout time.Duration) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	return &Client{
		httpClient:     &http.Client{Timeout: requestTimeout},
		apiKey:         apiKey,
		baseURL:        strings.TrimRight(baseURL, "/"),
		model:          model,
		maxAttempts:    maxAttempts,
		maxStateTokens: maxStateTokens,
	}
}

// Model reports the model id this client sends.
func (c *Client) Model() string { return c.model }

// Evaluate sends one request holding the state and every question. Questions
// run in parallel and each one sees the same state, so one call carries the
// whole fan-out.
//
// A 429, a 529, and any 5xx are retried with exponential backoff and the
// Retry-After header when the service sends one. A 401 and a 422 are returned
// to the caller at once, because a retry cannot fix either.
func (c *Client) Evaluate(ctx context.Context, state any, questions map[string]Question) (*Result, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("no questions to evaluate")
	}
	if estimated := estimateTokens(state); estimated > c.maxStateTokens {
		return nil, agentErrors.NewAPIError(0, fmt.Sprintf(
			"error: Jev state too large (estimated ~%d tokens, ceiling %d) — reduce the input set or turn the Jev layer off",
			estimated, c.maxStateTokens,
		))
	}

	payload := request{
		Model:     c.model,
		State:     state,
		Questions: questions,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, retryDelay(attempt, lastErr)); err != nil {
				return nil, err
			}
		}
		result, err := c.attempt(ctx, body)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

type request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

func (c *Client) attempt(ctx context.Context, body []byte) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("jev request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading jev response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, statusError(resp.StatusCode, raw, resp.Header.Get("Retry-After"))
	}

	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding jev response: %w", err)
	}
	return &result, nil
}

// statusError maps a non-2xx response onto a typed error. The message names the
// fix the caller can act on instead of forwarding the raw body.
func statusError(status int, raw []byte, retryAfter string) error {
	snippet := strings.TrimSpace(string(raw))
	if len(snippet) > 300 {
		snippet = snippet[:300] + "..."
	}
	var msg string
	switch status {
	case http.StatusUnauthorized:
		msg = "error: Jev rejected the API key (401) — set jev_api_key or export TYPESAFE_API_KEY"
	case http.StatusUnprocessableEntity:
		msg = fmt.Sprintf("error: Jev rejected the request (422): %s", snippet)
	default:
		msg = fmt.Sprintf("error: Jev API error (%d): %s", status, snippet)
	}
	err := agentErrors.NewAPIError(status, msg)
	if retryAfter != "" {
		return &retryError{err: err, after: retryAfter}
	}
	return err
}

// retryError carries a service-supplied Retry-After alongside the typed error
// so the backoff honours the service instead of guessing.
type retryError struct {
	err   error
	after string
}

func (e *retryError) Error() string { return e.err.Error() }
func (e *retryError) Unwrap() error { return e.err }

// retryable reports whether another attempt can plausibly succeed. A 429 and a
// 529 are documented as backoff cases; any other 5xx is treated the same way.
func retryable(err error) bool {
	var re *retryError
	if stderrors.As(err, &re) {
		return true
	}
	var apiErr *agentErrors.APIError
	if stderrors.As(err, &apiErr) {
		return apiErr.HTTPStatusCode >= 500
	}
	// A transport failure is retryable; a cancelled or expired context is not.
	if stderrors.Is(err, context.Canceled) || stderrors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// retryDelay returns the wait before the next attempt: the service-supplied
// Retry-After when present, otherwise exponential backoff.
func retryDelay(attempt int, lastErr error) time.Duration {
	var re *retryError
	if stderrors.As(lastErr, &re) {
		if d, err := time.ParseDuration(re.after); err == nil && d > 0 {
			return d
		}
		if secs, err := strconv.Atoi(re.after); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return retryBaseDelay << (attempt - 1)
}

// sleep waits for d, or returns the context error when the context ends first.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// estimateTokens approximates the token count of a state. Strings count their
// bytes; a structured state is marshalled first so its JSON size is measured.
func estimateTokens(state any) int {
	var size int
	switch s := state.(type) {
	case nil:
		return 0
	case string:
		size = len(s)
	case fmt.Stringer:
		size = len(s.String())
	default:
		if encoded, err := json.Marshal(s); err == nil {
			size = len(encoded)
		} else {
			size = len(fmt.Sprintf("%v", s))
		}
	}
	return size / 4
}
