package openai

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	goopenai "github.com/sashabaranov/go-openai"

	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

func TestClassifyAPIError_RateLimit(t *testing.T) {
	err := &goopenai.APIError{HTTPStatusCode: 429, Message: "rate limit exceeded"}
	result := classifyAPIError(err)
	if result == nil {
		t.Fatal("expected non-nil APIError for 429")
	}
	if result.HTTPStatusCode != 429 {
		t.Errorf("expected status 429, got %d", result.HTTPStatusCode)
	}
	if got := result.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestClassifyAPIError_Unauthorized(t *testing.T) {
	err := &goopenai.APIError{HTTPStatusCode: 401, Message: "invalid api key"}
	result := classifyAPIError(err)
	if result == nil {
		t.Fatal("expected non-nil APIError for 401")
	}
	if result.HTTPStatusCode != 401 {
		t.Errorf("expected status 401, got %d", result.HTTPStatusCode)
	}
}

func TestClassifyAPIError_BadRequest(t *testing.T) {
	err := &goopenai.APIError{HTTPStatusCode: 400, Message: "bad request"}
	result := classifyAPIError(err)
	if result == nil {
		t.Fatal("expected non-nil APIError for 400")
	}
	if result.HTTPStatusCode != 400 {
		t.Errorf("expected status 400, got %d", result.HTTPStatusCode)
	}
}

func TestClassifyAPIError_NotFound(t *testing.T) {
	err := &goopenai.APIError{HTTPStatusCode: 404, Message: "model not found"}
	result := classifyAPIError(err)
	if result == nil {
		t.Fatal("expected non-nil APIError for 404")
	}
	if result.HTTPStatusCode != 404 {
		t.Errorf("expected status 404, got %d", result.HTTPStatusCode)
	}
}

func TestClassifyAPIError_ServerError_ReturnsNil(t *testing.T) {
	err := &goopenai.APIError{HTTPStatusCode: 500, Message: "internal server error"}
	result := classifyAPIError(err)
	if result != nil {
		t.Errorf("expected nil for 500 (transient), got %+v", result)
	}
}

func TestClassifyAPIError_502_ReturnsNil(t *testing.T) {
	err := &goopenai.APIError{HTTPStatusCode: 502, Message: "bad gateway"}
	result := classifyAPIError(err)
	if result != nil {
		t.Errorf("expected nil for 502 (transient), got %+v", result)
	}
}

func TestClassifyAPIError_RequestError_RateLimit(t *testing.T) {
	err := &goopenai.RequestError{
		HTTPStatusCode: 429,
		Err:            fmt.Errorf("too many requests"),
	}
	result := classifyAPIError(err)
	if result == nil {
		t.Fatal("expected non-nil APIError for RequestError 429")
	}
	if result.HTTPStatusCode != 429 {
		t.Errorf("expected status 429, got %d", result.HTTPStatusCode)
	}
}

func TestClassifyAPIError_RequestError_ServerError_ReturnsNil(t *testing.T) {
	err := &goopenai.RequestError{
		HTTPStatusCode: 503,
		Err:            fmt.Errorf("service unavailable"),
	}
	result := classifyAPIError(err)
	if result != nil {
		t.Errorf("expected nil for RequestError 503 (transient), got %+v", result)
	}
}

func TestClassifyAPIError_GenericError_ReturnsNil(t *testing.T) {
	err := fmt.Errorf("connection refused")
	result := classifyAPIError(err)
	if result != nil {
		t.Errorf("expected nil for generic error, got %+v", result)
	}
}

func TestClassifyAPIError_WrappedAPIError(t *testing.T) {
	inner := &goopenai.APIError{HTTPStatusCode: 429, Message: "rate limited"}
	wrapped := fmt.Errorf("openai: %w", inner)
	result := classifyAPIError(wrapped)
	if result == nil {
		t.Fatal("expected non-nil APIError for wrapped 429")
	}
	if result.HTTPStatusCode != 429 {
		t.Errorf("expected status 429, got %d", result.HTTPStatusCode)
	}
}

func TestDetectResponseError_ErrorJSON(t *testing.T) {
	content := `{"error": {"message": "rate limit exceeded"}}`
	result := detectResponseError(content)
	if result == nil {
		t.Fatal("expected non-nil APIError for error JSON")
	}
	if result.HTTPStatusCode != 0 {
		t.Errorf("expected status 0, got %d", result.HTTPStatusCode)
	}
	if !strings.Contains(result.Message, "rate limit exceeded") {
		t.Errorf("expected message to contain 'rate limit exceeded', got: %s", result.Message)
	}
}

func TestDetectResponseError_ValidPlanJSON(t *testing.T) {
	content := `{"groups": [{"files": ["main.go"], "title": "feat: init"}]}`
	result := detectResponseError(content)
	if result != nil {
		t.Errorf("expected nil for valid plan JSON, got %+v", result)
	}
}

func TestDetectResponseError_PlainText(t *testing.T) {
	result := detectResponseError("I cannot help with that request")
	if result != nil {
		t.Errorf("expected nil for plain text, got %+v", result)
	}
}

func TestDetectResponseError_EmptyErrorMessage(t *testing.T) {
	content := `{"error": {"message": ""}}`
	result := detectResponseError(content)
	if result != nil {
		t.Errorf("expected nil for empty error message, got %+v", result)
	}
}

func TestDetectResponseError_EmptyString(t *testing.T) {
	result := detectResponseError("")
	if result != nil {
		t.Errorf("expected nil for empty string, got %+v", result)
	}
}

func TestAPIError_UnwrapsWithErrorsAs(t *testing.T) {
	apiErr := agentErrors.NewAPIError(429, "error: API rate limited (429): rate limited")
	wrapped := fmt.Errorf("generate commit message: %w", apiErr)
	doubleWrapped := fmt.Errorf("plan commits: %w", wrapped)

	var target *agentErrors.APIError
	if !errors.As(doubleWrapped, &target) {
		t.Fatal("errors.As should find *APIError through wrapping")
	}
	if target.HTTPStatusCode != 429 {
		t.Errorf("expected status 429, got %d", target.HTTPStatusCode)
	}
}
