package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/diff"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// stallHandler hijacks the connection, holds it open while reading bytes
// from the client until the client closes it, then exits. The read loop
// guarantees the goroutine returns promptly once the client side closes the
// TCP socket (which happens once the per-attempt context times out and the
// http.Transport tears down the connection), keeping the test goleak-clean.
func stallHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("http.ResponseWriter does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)
			return
		}
		defer conn.Close()
		// Block on Read; returns as soon as the client closes the socket.
		_, _ = io.Copy(io.Discard, conn)
	}
}

func TestClient_PerAttemptTimeout(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("http.ResponseWriter does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer server.Close()

	var buf bytes.Buffer
	c := NewClient("test-key", server.URL, "test-model", 1*time.Second, 0, &buf)

	start := time.Now()
	_, err := c.Generate(context.Background(), commit.GenerateRequest{
		Diff: &diff.StagedDiff{Files: []string{"main.go"}, Content: "+x", Lines: 1},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// Hotfix: the first timeout returns a typed error immediately. No retries
	// happen at the openai-client layer, so exactly one HTTP request reaches
	// the server and the call returns within a small multiple of the per-
	// attempt timeout.
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected exactly 1 HTTP request, got %d", got)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Generate took %s, expected close to the 1s per-attempt timeout", elapsed)
	}

	var target *commit.PlannerTimedOutError
	if !errors.As(err, &target) {
		t.Fatalf("expected *commit.PlannerTimedOutError, got %T: %v", err, err)
	}
	if target.Model != "test-model" {
		t.Errorf("expected Model=test-model, got %q", target.Model)
	}
	if target.Timeout != 1*time.Second {
		t.Errorf("expected Timeout=1s, got %s", target.Timeout)
	}
	if !errors.Is(err, commit.ErrPlannerTimedOut) {
		t.Errorf("expected errors.Is(err, commit.ErrPlannerTimedOut) = true")
	}

	msg := err.Error()
	if strings.Contains(msg, "context.DeadlineExceeded") {
		t.Errorf("error leaks raw 'context.DeadlineExceeded', got: %q", msg)
	}
	if strings.Contains(msg, "panic") {
		t.Errorf("error contains 'panic', got: %q", msg)
	}
}

// TestClient_TimeoutReturnsTypedError locks in the hotfix contract: the per-
// attempt deadline elapsing surfaces as a *commit.PlannerTimedOutError
// populated with the configured model and timeout. Used by the cmd layer to
// render an actionable diagnostic naming both fields.
func TestClient_TimeoutReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(stallHandler(t))
	defer server.Close()

	c := NewClient("test-key", server.URL, "qwen3.6-flash", 750*time.Millisecond, 0, nil)

	_, err := c.Plan(context.Background(), commit.PlanRequest{
		StagedDiff:   &diff.StagedDiff{Files: []string{"main.go"}},
		UnstagedDiff: &diff.StagedDiff{},
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	var target *commit.PlannerTimedOutError
	if !errors.As(err, &target) {
		t.Fatalf("expected *commit.PlannerTimedOutError, got %T: %v", err, err)
	}
	if target.Model != "qwen3.6-flash" {
		t.Errorf("expected Model=qwen3.6-flash, got %q", target.Model)
	}
	if target.Timeout != 750*time.Millisecond {
		t.Errorf("expected Timeout=750ms, got %s", target.Timeout)
	}
}

// slowResponseHandler sleeps before responding with a canned chat-completion
// payload. Used to count heartbeat ticks fired while the request is in flight.
func slowResponseHandler(sleep time.Duration) http.HandlerFunc {
	body := `{
  "id": "chatcmpl-test",
  "object": "chat.completion",
  "created": 0,
  "model": "test-model",
  "choices": [
    {"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": "{\"title\":\"feat: x\",\"bullets\":[\"X\"],\"explanation\":\"E.\"}"}}
  ]
}`
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(sleep):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestClient_HeartbeatTicks(t *testing.T) {
	server := httptest.NewServer(slowResponseHandler(350 * time.Millisecond))
	defer server.Close()

	var buf bytes.Buffer
	// requestTimeout generous enough that the call returns normally; heartbeat
	// interval of 100ms gives ~3 ticks in 350ms.
	c := NewClient("test-key", server.URL, "test-model", 5*time.Second, 100*time.Millisecond, &buf)

	_, err := c.Generate(context.Background(), commit.GenerateRequest{
		Diff: &diff.StagedDiff{Files: []string{"main.go"}, Content: "+x", Lines: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tickRe := regexp.MustCompile(`(?m)^still waiting on LLM\.\.\. \(\d+s elapsed, model=test-model\)$`)
	matches := tickRe.FindAllString(buf.String(), -1)
	if len(matches) < 2 || len(matches) > 4 {
		t.Errorf("expected 2-4 heartbeat ticks in 350ms, got %d. output:\n%s", len(matches), buf.String())
	}
	for _, line := range matches {
		if !strings.Contains(line, "model=test-model") {
			t.Errorf("tick line missing model: %q", line)
		}
	}
}

// TestClient_HeartbeatNoSecretLeakage locks in REQ-011: the heartbeat ticker
// emits only elapsed-time and model metadata. The API key and base URL host
// must never appear in the captured output buffer, regardless of how many
// ticks fire while the request is in flight.
func TestClient_HeartbeatNoSecretLeakage(t *testing.T) {
	const (
		apiKey    = "sk-secret-key-redact-me-001"
		baseHost  = "proxy.example.com"
		modelName = "gpt-x"
	)
	server := httptest.NewServer(slowResponseHandler(200 * time.Millisecond))
	defer server.Close()

	var buf bytes.Buffer
	// The request still goes to the httptest server URL; `baseHost` is a
	// sentinel checked against the captured buffer — the heartbeat line
	// should reference only the model, never the endpoint.
	c := NewClient(apiKey, server.URL, modelName, 5*time.Second, 50*time.Millisecond, &buf)

	_, err := c.Generate(context.Background(), commit.GenerateRequest{
		Diff: &diff.StagedDiff{Files: []string{"main.go"}, Content: "+x", Lines: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	tickRe := regexp.MustCompile(`(?m)^still waiting on LLM\.\.\.`)
	if len(tickRe.FindAllString(out, -1)) < 2 {
		t.Fatalf("expected at least 2 heartbeat ticks in 200ms with 50ms interval, got:\n%s", out)
	}

	if strings.Contains(out, apiKey) {
		t.Errorf("heartbeat output leaked API key %q:\n%s", apiKey, out)
	}
	if strings.Contains(out, baseHost) {
		t.Errorf("heartbeat output leaked base URL host %q:\n%s", baseHost, out)
	}
}

func TestClient_HeartbeatNoOpWhenOutNil(t *testing.T) {
	server := httptest.NewServer(slowResponseHandler(50 * time.Millisecond))
	defer server.Close()

	c := NewClient("test-key", server.URL, "test-model", 5*time.Second, 10*time.Millisecond, nil)

	// Just verify no panic and request completes.
	_, err := c.Generate(context.Background(), commit.GenerateRequest{
		Diff: &diff.StagedDiff{Files: []string{"main.go"}, Content: "+x", Lines: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_RequestTimeoutDefaultsTo90s(t *testing.T) {
	c := NewClient("test-key", "http://127.0.0.1:1", "test-model", 0, 0, nil)
	if c.requestTimeout != 90*time.Second {
		t.Errorf("expected default requestTimeout=90s, got %s", c.requestTimeout)
	}
	if c.heartbeatInterval != 15*time.Second {
		t.Errorf("expected default heartbeatInterval=15s, got %s", c.heartbeatInterval)
	}
}

// lengthHandler returns a canned chat-completion response whose single choice
// reports finish_reason=length. Used to drive the token-doubling retry loop
// in callLLM. The handler atomically increments count on every request so the
// test can assert the exact number of inbound HTTP attempts.
func lengthHandler(count *int32) http.HandlerFunc {
	body := `{
  "id": "chatcmpl-length",
  "object": "chat.completion",
  "created": 0,
  "model": "deepseek-v4-flash",
  "choices": [
    {"index": 0, "finish_reason": "length", "message": {"role": "assistant", "content": "{\"groups\":["}}
  ]
}`
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(count, 1)
		// Echo back the request so the test could decode it if needed; the
		// canned body is independent of the request payload.
		_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestClient_TokenCeiling(t *testing.T) {
	var count int32
	server := httptest.NewServer(lengthHandler(&count))
	defer server.Close()

	c := NewClient("test-key", server.URL, "deepseek-v4-flash", 5*time.Second, 0, nil)

	_, err := c.Plan(context.Background(), commit.PlanRequest{
		StagedDiff:   &diff.StagedDiff{Files: []string{"main.go"}},
		UnstagedDiff: &diff.StagedDiff{},
	})

	if err == nil {
		t.Fatal("expected PlannerBudgetExhaustedError, got nil")
	}
	// Plan seeds maxTokens=8192 and ceiling=16384. The first attempt doubles
	// to 16384 (allowed). The second attempt would double to 32768, which
	// exceeds the ceiling — so the loop bails out before a third HTTP call.
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Fatalf("expected exactly 2 HTTP requests (one allowed double), got %d", got)
	}

	var target *commit.PlannerBudgetExhaustedError
	if !errors.As(err, &target) {
		t.Fatalf("expected error to be *commit.PlannerBudgetExhaustedError, got %T: %v", err, err)
	}
	if target.Model != "deepseek-v4-flash" {
		t.Errorf("expected Model=deepseek-v4-flash, got %q", target.Model)
	}
	if target.Ceiling != 16384 {
		t.Errorf("expected Ceiling=16384, got %d", target.Ceiling)
	}
	if !errors.Is(err, commit.ErrPlannerBudgetExhausted) {
		t.Error("expected errors.Is(err, commit.ErrPlannerBudgetExhausted) = true")
	}
}

// Sanity check that the stallHandler actually stalls — protects the timeout
// test from passing trivially if hijack stops working.
func TestStallHandler_DoesStall(t *testing.T) {
	server := httptest.NewServer(stallHandler(t))
	defer server.Close()

	d := net.Dialer{Timeout: 1 * time.Second}
	conn, err := d.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.0\r\n\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 16)
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected read deadline error from stalled server, got data")
	}
	var nerr net.Error
	if !errors.As(err, &nerr) || !nerr.Timeout() {
		t.Fatalf("expected timeout net.Error, got %v", err)
	}
}

// A fresh repository with no history or tracked files legitimately yields no
// scopes. GenerateScopes must return the empty list without error so that
// `git agent init --scope` succeeds instead of bailing out.
func TestClient_GenerateScopes_EmptyIsNotAnError(t *testing.T) {
	body := `{
  "id": "chatcmpl-test",
  "object": "chat.completion",
  "created": 0,
  "model": "test-model",
  "choices": [
    {"index": 0, "finish_reason": "stop", "message": {"role": "assistant", "content": "{\"scopes\": [], \"reasoning\": \"no history\"}"}}
  ]
}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	c := NewClient("test-key", server.URL, "test-model", 5*time.Second, 0, nil)

	scopes, reasoning, err := c.GenerateScopes(context.Background(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error for empty scopes, got %v", err)
	}
	if len(scopes) != 0 {
		t.Errorf("expected empty scopes, got %d: %v", len(scopes), scopes)
	}
	if reasoning != "no history" {
		t.Errorf("expected reasoning preserved, got %q", reasoning)
	}
}
