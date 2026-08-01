package openai

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	goopenai "github.com/sashabaranov/go-openai"

	"github.com/gitagenthq/git-agent/domain/commit"
	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
	"github.com/gitagenthq/git-agent/domain/project"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

// Default transport-level bounds applied when the caller passes a non-positive
// value. Mirror the constants in infrastructure/config/resolver.go so a
// zero-config client behaves identically to a fully-resolved one.
const (
	defaultRequestTimeout    = 90 * time.Second
	defaultHeartbeatInterval = 15 * time.Second
)

// Per-endpoint upper bounds on MaxCompletionTokens. callLLM doubles the budget
// on every finish_reason=length retry; once the next double would exceed the
// matching ceiling, callLLM returns a *commit.PlannerBudgetExhaustedError
// instead of pinging the endpoint a third time. Keeping ceilings as constants
// (per endpoint, not per client) makes the upper bound visible in code review
// and avoids a config knob nobody would tune.
const (
	planMaxTokensCeiling     = 16384
	generateMaxTokensCeiling = 16384
	scopesMaxTokensCeiling   = 16384
	detectMaxTokensCeiling   = 4096

	// maxInputTokensCeiling is the preflight bound on the combined system +
	// user prompt. It mirrors the largest context window offered by the
	// supported endpoints (AI Gateway / Gemini / DeepSeek all top out at 1M
	// tokens); a request above this estimate is rejected before the HTTP call
	// instead of burning a doomed request on the provider. Models with smaller
	// context windows are covered by the 400 token-limit classification, which
	// turns the provider's rejection into actionable guidance.
	maxInputTokensCeiling = 1_000_000
)

type Client struct {
	inner             *goopenai.Client
	model             string
	requestTimeout    time.Duration
	heartbeatInterval time.Duration
	out               io.Writer
}

func NewClient(
	apiKey, baseURL, model string,
	requestTimeout, heartbeatInterval time.Duration,
	out io.Writer,
) *Client {
	cfg := goopenai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultHeartbeatInterval
	}
	cfg.HTTPClient = &http.Client{Timeout: requestTimeout}
	c := &Client{
		inner:             goopenai.NewClientWithConfig(cfg),
		model:             model,
		requestTimeout:    requestTimeout,
		heartbeatInterval: heartbeatInterval,
		out:               out,
	}
	return c
}

// RequestTimeout reports the per-attempt HTTP timeout this client applies to
// outbound LLM requests. Exposed so cmd-layer wiring tests can confirm config
// values reach the transport.
func (c *Client) RequestTimeout() time.Duration { return c.requestTimeout }

// HeartbeatInterval reports the cadence at which heartbeat lines are emitted
// while an LLM call is in flight.
func (c *Client) HeartbeatInterval() time.Duration { return c.heartbeatInterval }

// heartbeat emits a single "still waiting on LLM..." line per tick while an
// LLM request is in flight. The goroutine exits within one tick of either the
// done channel closing or the context being cancelled. When out is nil it
// returns immediately, making it a no-op for tests and headless contexts.
func (c *Client) heartbeat(ctx context.Context, done <-chan struct{}) {
	if c.out == nil {
		return
	}
	t := time.NewTicker(c.heartbeatInterval)
	defer t.Stop()
	start := time.Now()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			fmt.Fprintf(c.out, "still waiting on LLM... (%ds elapsed, model=%s)\n",
				int(time.Since(start).Seconds()), c.model)
		}
	}
}

func extractJSON(s string) string {
	// Pick whichever of '{' or '[' appears first.
	open, close := byte(0), byte(0)
	start := -1
	for _, pair := range [][2]byte{{'{', '}'}, {'[', ']'}} {
		idx := strings.IndexByte(s, pair[0])
		if idx != -1 && (start == -1 || idx < start) {
			open, close = pair[0], pair[1]
			start = idx
		}
	}
	if start == -1 {
		return s
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		ch := s[i]
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s
}

// unmarshalLLMJSON extracts JSON from a raw LLM response and unmarshals it
// into dest. If wrapKey is non-empty and the extracted JSON is a bare array,
// it wraps the array as {wrapKey: <array>} and retries — handling the common
// case where the LLM omits the expected wrapper object.
func unmarshalLLMJSON(raw, wrapKey string, dest any) error {
	cleaned := extractJSON(raw)
	if err := json.Unmarshal([]byte(cleaned), dest); err != nil {
		if wrapKey != "" && len(cleaned) > 0 && cleaned[0] == '[' {
			wrapped := `{"` + wrapKey + `":` + cleaned + `}`
			if err2 := json.Unmarshal([]byte(wrapped), dest); err2 == nil {
				return nil
			}
		}
		return fmt.Errorf("parse response json: %w\nraw: %s", err, cleaned)
	}
	return nil
}

// AllSystemPrompts returns every static system prompt sent by this client.
// The returned slice is the source of truth for the proxy's ALLOWED_SYSTEM_PROMPTS
// secret. To sync: git-agent config prompts | wrangler secret put ALLOWED_SYSTEM_PROMPTS
func AllSystemPrompts() []string {
	return []string{
		generateSystemPrompt,
		generateSystemPromptScoped,
		retrySystemPrompt,
		planSystemPrompt,
		planSystemPromptScoped,
		detectTechSystemPrompt,
		generateScopesSystemPrompt,
	}
}

const generateSystemPrompt = `You are an expert software engineer. Generate a conventional commit message from the provided git diff. Respond ONLY with valid JSON in this exact format: {"title": "...", "bullets": ["Bullet one", "Bullet two"], "explanation": "Explanation paragraph."}. Rules: title uses conventional commits format with one of these types: feat, fix, docs, style, refactor, perf, test, chore, build, ci, revert — ALL LOWERCASE ≤50 chars imperative mood; scope is optional, omit if no clear scope applies; bullets is an array of strings each starting with an UPPERCASE first letter, imperative mood, targeting ≤72 chars per entry; explanation is a closing paragraph in sentence case; all text targets ≤72 characters per line.`

const generateSystemPromptScoped = `You are an expert software engineer. Generate a conventional commit message from the provided git diff. Respond ONLY with valid JSON in this exact format: {"title": "...", "bullets": ["Bullet one", "Bullet two"], "explanation": "Explanation paragraph."}. Rules: title uses conventional commits format with one of these types: feat, fix, docs, style, refactor, perf, test, chore, build, ci, revert — ALL LOWERCASE ≤50 chars imperative mood; REQUIRED scope — you MUST use one of the scopes listed in the user message; choose by reading each scope's DESCRIPTION to see what it covers, not by keyword similarity with the scope name; if no listed scope covers the change, omit the scope rather than using a mismatched one; bullets is an array of strings each starting with an UPPERCASE first letter, imperative mood, targeting ≤72 chars per entry; explanation is a closing paragraph in sentence case; all text targets ≤72 characters per line.`

const retrySystemPrompt = `You are an expert software engineer. Fix the commit message to satisfy the hook requirement. Respond ONLY with valid JSON: {"title": "...", "bullets": ["Bullet one", "Bullet two"], "explanation": "Explanation paragraph."}. Title: conventional commits format ALL LOWERCASE ≤50 chars imperative mood. Bullets: array of strings each starting with UPPERCASE first letter, imperative mood, ≤72 chars per entry. Explanation: closing paragraph, sentence case. All text targets ≤72 characters per line.`

const planSystemPrompt = `You are an expert software engineer. Analyse the provided file paths and split them into meaningful atomic commits.

If a PRIMARY DIRECTIVE is given, it is the most important constraint: only include files directly relevant to it; put those files in group 0; leave all unrelated files out.
If there are staged files and no PRIMARY DIRECTIVE, they MUST be group 0 (respect user intent).
Split remaining changes by logical concern (feature, bug fix, refactor, test, docs, etc.) — infer the nature of each change from the file path, name, and directory structure.
Each group should be a cohesive unit of change.
Some entries look like "path/to/dir/ (N files)" instead of a real file path — this means N files under that directory were too numerous to list individually. Treat it as one inseparable unit: copy the label into "files" EXACTLY as shown, on its own, in whichever group that directory belongs to. Never split it apart or invent individual file names for it.

Respond ONLY with valid JSON:
{"groups": [{"files": ["..."], "title": "type(scope): description", "bullets": ["Bullet one"], "explanation": "Explanation."}]}

Rules for title: conventional commits format, ALL LOWERCASE, ≤50 chars, imperative mood.
Scope is optional; omit if no clear scope applies.
Rules for bullets: array of strings, each starting with UPPERCASE first letter, imperative mood, ≤72 chars per entry.
Rules for explanation: closing paragraph, sentence case, ≤72 chars per line.`

const planSystemPromptScoped = `You are an expert software engineer. Analyse the provided file paths and split them into meaningful atomic commits.

If a PRIMARY DIRECTIVE is given, it is the most important constraint: only include files directly relevant to it; put those files in group 0; leave all unrelated files out.
If there are staged files and no PRIMARY DIRECTIVE, they MUST be group 0 (respect user intent).
Split remaining changes by logical concern (feature, bug fix, refactor, test, docs, etc.) — infer the nature of each change from the file path, name, and directory structure.
Each group should be a cohesive unit of change.
Some entries look like "path/to/dir/ (N files)" instead of a real file path — this means N files under that directory were too numerous to list individually. Treat it as one inseparable unit: copy the label into "files" EXACTLY as shown, on its own, in whichever group that directory belongs to. Never split it apart or invent individual file names for it.

Respond ONLY with valid JSON:
{"groups": [{"files": ["..."], "title": "type(scope): description", "bullets": ["Bullet one"], "explanation": "Explanation."}]}

Rules for title: conventional commits format, ALL LOWERCASE, ≤50 chars, imperative mood.
REQUIRED scope — every title MUST use one of the scopes listed in the user message; choose by reading each scope's DESCRIPTION to see what it covers, not by keyword similarity with the scope name. Files that map to different scopes MUST be placed in separate groups — never mix scopes within one group. If NO listed scope covers a group's files (e.g. documentation-only changes), omit the scope for that group rather than forcing a mismatched one. Never return an empty groups array when files are provided.
Rules for bullets: array of strings, each starting with UPPERCASE first letter, imperative mood, ≤72 chars per entry.
Rules for explanation: closing paragraph, sentence case, ≤72 chars per line.`

const detectTechSystemPrompt = `You are an expert software engineer. Analyze the project's OS, directories, and files to detect which technologies are used.

Return a JSON object with a "technologies" array containing only valid Toptal gitignore API identifiers.
Respond ONLY with valid JSON: {"technologies": ["go", "node", "visualstudiocode"]}

Rules:
- Include the OS identifier (e.g. "macos", "linux", "windows")
- Include programming languages detected from file extensions
- Include build tools, editors, and IDEs if evidence exists
- Use lowercase Toptal API identifiers only (e.g. "go", "node", "python", "rust", "jetbrains", "visualstudiocode")
- Use exact Toptal identifiers for build tools: "makefile" for GNU Make (NOT "make"), "cmake" for CMake
- Do NOT include technologies with no evidence in the project files`

const generateScopesSystemPrompt = `You are an expert software engineer. Derive commit scopes from the top-level directories of the project, using commit history to validate and refine them.

Respond ONLY with valid JSON: {"scopes": [{"name": "...", "description": "..."}], "reasoning": "..."}

Rules (STRICTLY enforce):
- Generate exactly one scope per meaningful top-level directory listed in "Top-level directories"
- NEVER create scopes for subdirectories — each top-level directory is a single scope that covers all its contents (e.g. "infrastructure/" is one scope "infra", NOT separate scopes for infrastructure/openai/, infrastructure/git/, etc.)
- Skip dependency/build/generated directories (node_modules, vendor, dist, build, target, __pycache__, .next, out, coverage)
- Skip documentation and asset directories (docs, doc, documentation, assets, static, public, resources)
- Use the commit log (subject + changed files) to understand which directories represent distinct concerns and how they are named in practice
- ALL scope names MUST be short — single words or abbreviations only
- Single-word names: use as-is, EXCEPT apply well-known short forms for long words ("application" -> "app", "infrastructure" -> "infra", "cmd" -> "cli")
- Hyphenated or multi-word names: MUST convert to initials/acronym ("git-agent-proxy" -> "gap", "my-frontend" -> "mf"); use the final segment only when it is already short and unambiguous on its own
- If commit history shows a consistent scope abbreviation for a directory, prefer that abbreviation over any derived form
- NEVER invent scopes from file names or internal package names (e.g. do NOT derive "cs" from "commit_service.go")
- NEVER use commit types (feat, fix, chore, docs, refactor, test, style, perf) as scopes
- All scope names lowercase
- Aim for 5–10 scopes total — merge or skip marginal directories to avoid scope bloat
- Each scope MUST have a "description" field: a concise phrase (under 20 words) that (1) names the key features/responsibilities the scope covers, (2) includes the source directory path naturally, and (3) explicitly states what the scope does NOT cover when its name could be confused with another scope. Example: scope "infra" description: "git, OpenAI, and config adapters in infrastructure/; does not cover CLI commands". Do NOT use "dir/ — text" prefix format. Descriptions are the primary signal AI uses to pick the correct scope when generating commit messages — vague or overlapping descriptions cause scope misassignment
- When "Existing scopes" are provided, treat them as historical context for naming conventions only — do NOT blindly preserve them. Drop any existing scope that violates the rules above`

// callLLM sends a chat completion request with retry logic for transient failures and empty responses.
func (c *Client) callLLM(ctx context.Context, system, user string, maxTokens, maxTokensCeiling int) (string, error) {
	const maxAttempts = 3

	// Preflight input-size guard: refuse a request that is already larger than
	// the largest supported context before paying for a doomed HTTP call. The
	// byte-per-4 estimate is conservative for code and English text; CJK-heavy
	// prompts tokenize denser, so the estimate can undershoot — those land on
	// the provider's 400 classification instead, which still yields the same
	// actionable message.
	if estimated := (len(system) + len(user)) / 4; estimated > maxInputTokensCeiling {
		return "", agentErrors.NewAPIError(0, fmt.Sprintf(
			"error: LLM input too large (estimated ~%d tokens, ceiling %d) — reduce the staged diff (commit fewer files at once) or lower --max-diff-bytes / max_diff_bytes",
			estimated, maxInputTokensCeiling,
		))
	}

	msgs := []goopenai.ChatCompletionMessage{
		{Role: goopenai.ChatMessageRoleSystem, Content: system},
		{Role: goopenai.ChatMessageRoleUser, Content: user},
	}

	req := goopenai.ChatCompletionRequest{
		Model:               c.model,
		Messages:            msgs,
		MaxCompletionTokens: maxTokens,
		Temperature:         0,
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
		done := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.heartbeat(attemptCtx, done)
		}()

		resp, err := c.inner.CreateChatCompletion(attemptCtx, req)
		close(done)
		wg.Wait()
		cancel()

		if err != nil {
			// Caller cancelled (SIGINT) — propagate without retry so the
			// process exits promptly.
			if stderrors.Is(ctx.Err(), context.Canceled) || stderrors.Is(err, context.Canceled) {
				return "", err
			}
			// Per-attempt deadline elapsed: return a typed error on the
			// FIRST timeout, with no retry. Retrying with the same per-
			// attempt timeout produces the same outcome — the model
			// genuinely needed >timeout to respond — and the prior
			// behaviour of three serial timeouts (4.5 min at the default
			// 90s) wastes the caller's wall-clock for zero new signal.
			// The cmd-layer translates the typed error into an actionable
			// diagnostic.
			if stderrors.Is(err, context.DeadlineExceeded) {
				return "", &commit.PlannerTimedOutError{
					Model:   c.model,
					Timeout: c.requestTimeout,
				}
			}
			// Check for non-transient API errors that should not be retried.
			if apiErr := classifyAPIError(err); apiErr != nil {
				return "", apiErr
			}
			lastErr = fmt.Errorf("openai chat completion: %w", err)
			continue
		}

		// Token exhaustion (finish_reason=length) — double the budget and
		// retry regardless of whether the response is empty or partial.
		// Empty: reasoning models may spend all tokens on chain-of-thought.
		// Partial: output is likely truncated (e.g. incomplete JSON).
		if len(resp.Choices) > 0 && resp.Choices[0].FinishReason == goopenai.FinishReasonLength {
			next := req.MaxCompletionTokens * 2
			if next > maxTokensCeiling {
				return "", &commit.PlannerBudgetExhaustedError{
					Model:   c.model,
					Ceiling: maxTokensCeiling,
				}
			}
			req.MaxCompletionTokens = next
			lastErr = fmt.Errorf("LLM exhausted token limit at %d (model=%s, attempt=%d/%d)",
				req.MaxCompletionTokens/2, c.model, attempt+1, maxAttempts)
			continue
		}

		if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
			lastErr = fmt.Errorf("LLM returned empty response (model=%s, attempt=%d/%d)", c.model, attempt+1, maxAttempts)
			continue
		}

		content := resp.Choices[0].Message.Content
		if apiErr := detectResponseError(content); apiErr != nil {
			return "", apiErr
		}
		return content, nil
	}
	return "", lastErr
}

// detectResponseError checks if a successful LLM response body contains an
// error payload (e.g., a gateway returning 200 OK with {"error": {...}}).
// Returns an *agentErrors.APIError if detected, nil otherwise.
func detectResponseError(content string) *agentErrors.APIError {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var probe struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil
	}
	if probe.Error.Message != "" {
		return agentErrors.NewAPIError(0,
			fmt.Sprintf("error: API returned error in response body: %s", probe.Error.Message))
	}
	return nil
}

// tokenLimitPatterns are substrings shared by provider "input too large"
// rejections (Gemini, DeepSeek, OpenAI-compatible gateways). Matched against
// the 400 message to rewrite it into actionable guidance instead of a raw
// provider echo.
var tokenLimitPatterns = []string{
	"token count exceeds",
	"maximum context",
	"context length",
	"context window",
	"maximum number of tokens allowed",
}

// isTokenLimitMessage reports whether a provider error message indicates the
// request exceeded the model's input token limit.
func isTokenLimitMessage(msg string) bool {
	lower := strings.ToLower(msg)
	for _, p := range tokenLimitPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// classifyAPIError inspects an error from the go-openai library and returns a
// typed *agentErrors.APIError for non-transient failures (rate limit, auth,
// bad request, etc.) that should NOT be retried. Returns nil for transient
// errors that the caller's retry loop should handle.
func classifyAPIError(err error) *agentErrors.APIError {
	var apiErr *goopenai.APIError
	if ok := stderrors.As(err, &apiErr); ok {
		switch apiErr.HTTPStatusCode {
		case http.StatusTooManyRequests:
			return agentErrors.NewAPIError(apiErr.HTTPStatusCode,
				fmt.Sprintf("error: API rate limited (429): %s", apiErr.Message))
		case http.StatusUnauthorized:
			return agentErrors.NewAPIError(apiErr.HTTPStatusCode,
				fmt.Sprintf("error: API authentication failed (401): %s", apiErr.Message))
		case http.StatusForbidden:
			return agentErrors.NewAPIError(apiErr.HTTPStatusCode,
				fmt.Sprintf("error: API access denied (403): %s", apiErr.Message))
		case http.StatusBadRequest:
			if isTokenLimitMessage(apiErr.Message) {
				return agentErrors.NewAPIError(apiErr.HTTPStatusCode,
					fmt.Sprintf("error: API bad request (400): the model's input token limit was exceeded (%s) — reduce the staged diff (commit fewer files at once), lower --max-diff-bytes / max_diff_bytes, or configure a model with a larger context window",
						strings.TrimSpace(apiErr.Message)))
			}
			return agentErrors.NewAPIError(apiErr.HTTPStatusCode,
				fmt.Sprintf("error: API bad request (400): %s", apiErr.Message))
		case http.StatusNotFound:
			return agentErrors.NewAPIError(apiErr.HTTPStatusCode,
				fmt.Sprintf("error: API endpoint or model not found (404): %s", apiErr.Message))
		default:
			if apiErr.HTTPStatusCode >= 400 && apiErr.HTTPStatusCode < 500 {
				return agentErrors.NewAPIError(apiErr.HTTPStatusCode,
					fmt.Sprintf("error: API error (%d): %s", apiErr.HTTPStatusCode, apiErr.Message))
			}
		}
		// 5xx errors are transient — let the retry loop handle them.
		return nil
	}

	var reqErr *goopenai.RequestError
	if ok := stderrors.As(err, &reqErr); ok {
		if reqErr.HTTPStatusCode == http.StatusTooManyRequests {
			return agentErrors.NewAPIError(reqErr.HTTPStatusCode,
				fmt.Sprintf("error: API rate limited (429): %s", reqErr.Error()))
		}
		if reqErr.HTTPStatusCode >= 400 && reqErr.HTTPStatusCode < 500 {
			return agentErrors.NewAPIError(reqErr.HTTPStatusCode,
				fmt.Sprintf("error: API error (%d): %s", reqErr.HTTPStatusCode, reqErr.Error()))
		}
	}

	return nil
}

func (c *Client) Generate(ctx context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error) {
	var systemPrompt, userPrompt string

	if req.PreviousMessage != "" && req.HookFeedback != "" {
		systemPrompt = retrySystemPrompt
		userPrompt = fmt.Sprintf(
			"Fix the following commit message:\n\n%s\n\nThe commit hook rejected it for this reason:\n%s\n\nRewrite the message to satisfy the requirement. Keep the semantic content unchanged.",
			req.PreviousMessage,
			req.HookFeedback,
		)
	} else {
		hasScopes := req.Config != nil && len(req.Config.Scopes) > 0
		if hasScopes {
			systemPrompt = generateSystemPromptScoped
		} else {
			systemPrompt = generateSystemPrompt
		}

		var promptParts []string
		if req.Intent != "" {
			promptParts = append(promptParts, "PRIMARY DIRECTIVE — focus only on this: "+req.Intent)
		}
		if hasScopes {
			promptParts = append(promptParts, "REQUIRED scopes (match by description, not name):\n- "+req.Config.FormatScopesForLLM())
		}
		promptParts = append(promptParts, fmt.Sprintf("Git diff:\n<diff>\n%s\n</diff>\n\nStaged files: %s",
			req.Diff.Content,
			strings.Join(req.Diff.Files, ", "),
		))
		userPrompt = strings.Join(promptParts, "\n\n")
		if req.HookFeedback != "" {
			userPrompt += "\n\nPrevious attempt was rejected by the commit hook. Reason:\n" + req.HookFeedback + "\nFix the commit message to satisfy the requirement above."
		}
	}

	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		raw, err := c.callLLM(ctx, systemPrompt, userPrompt, 4096, generateMaxTokensCeiling)
		if err != nil {
			return nil, err
		}

		var result struct {
			Title       string   `json:"title"`
			Bullets     []string `json:"bullets"`
			Explanation string   `json:"explanation"`
		}
		if err := unmarshalLLMJSON(raw, "", &result); err != nil {
			lastErr = err
			continue
		}
		if result.Title == "" {
			lastErr = fmt.Errorf("LLM returned empty commit message\nraw: %s", extractJSON(raw))
			continue
		}

		return &commit.CommitMessage{
			Title:       result.Title,
			Bullets:     result.Bullets,
			Explanation: commit.WrapExplanation(strings.ReplaceAll(result.Explanation, `\n`, "\n"), 72),
		}, nil
	}
	return nil, lastErr
}

func (c *Client) Plan(ctx context.Context, req commit.PlanRequest) (*commit.CommitPlan, error) {
	hasScopes := req.Config != nil && len(req.Config.Scopes) > 0

	groups, raw, err := c.planOnce(ctx, req, hasScopes)
	if err != nil {
		return nil, err
	}

	// A scoped plan can come back empty when no configured scope covers the
	// changed files: the scoped prompt forbids unlisted scopes, and scope
	// generation deliberately skips docs/asset directories, so a docs-only
	// changeset leaves the LLM with no legal grouping. Retry once without
	// the scope constraint instead of failing the commit.
	if len(groups) == 0 && hasScopes {
		groups, raw, err = c.planOnce(ctx, req, false)
		if err != nil {
			return nil, err
		}
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("LLM returned empty plan (no commit groups)\nraw: %s", extractJSON(raw))
	}

	plan := &commit.CommitPlan{}
	for _, g := range groups {
		plan.Groups = append(plan.Groups, commit.CommitGroup{
			Files: g.Files,
			Message: commit.CommitMessage{
				Title:       g.Title,
				Bullets:     g.Bullets,
				Explanation: commit.WrapExplanation(strings.ReplaceAll(g.Explanation, `\n`, "\n"), 72),
			},
		})
	}
	return plan, nil
}

// planGroup mirrors one entry of the planner's {"groups": [...]} response.
type planGroup struct {
	Files       []string `json:"files"`
	Title       string   `json:"title"`
	Bullets     []string `json:"bullets"`
	Explanation string   `json:"explanation"`
}

// planOnce sends a single plan request — scoped or unscoped — and returns the
// parsed groups plus the raw LLM payload for error reporting.
func (c *Client) planOnce(ctx context.Context, req commit.PlanRequest, scoped bool) ([]planGroup, string, error) {
	systemPrompt := planSystemPrompt
	if scoped {
		systemPrompt = planSystemPromptScoped
	}

	maxPlanFiles := req.MaxPlanFiles
	if maxPlanFiles <= 0 {
		maxPlanFiles = commit.DefaultMaxPlanFiles
	}
	expand := map[string][]string{}

	var planParts []string
	if req.Intent != "" {
		planParts = append(planParts, "PRIMARY DIRECTIVE — focus only on this: "+req.Intent)
	}
	if scoped {
		planParts = append(planParts, "REQUIRED scopes (match by description, not name):\n- "+req.Config.FormatScopesForLLM())
	}
	if req.StagedDiff != nil && len(req.StagedDiff.Files) > 0 {
		summary := commit.SummarizeFileList(req.StagedDiff.Files, maxPlanFiles)
		labels := mergeFileListExpansion(expand, summary)
		planParts = append(planParts, fmt.Sprintf("Staged files (already staged by user — keep as group 0):\n%s",
			strings.Join(labels, "\n"),
		))
	}
	if req.UnstagedDiff != nil && len(req.UnstagedDiff.Files) > 0 {
		summary := commit.SummarizeFileList(req.UnstagedDiff.Files, maxPlanFiles)
		labels := mergeFileListExpansion(expand, summary)
		planParts = append(planParts, fmt.Sprintf("Unstaged files:\n%s",
			strings.Join(labels, "\n"),
		))
	}
	if len(req.CoChangeHints) > 0 {
		var lines []string
		for _, h := range req.CoChangeHints {
			line := fmt.Sprintf("- %s <-> %s (%.0f%%)", h.FileA, h.FileB, h.Strength*100)
			if len(h.Subjects) > 0 {
				// Surface the reason they coupled, e.g. once committed together as
				// "feat(auth): add token refresh" — semantic signal, not just a count.
				line += fmt.Sprintf(" — once committed together as: %q", strings.Join(h.Subjects, "; "))
			}
			lines = append(lines, line)
		}
		planParts = append(planParts, "Historical co-change — these file pairs are usually committed together. Keep each pair in the SAME commit group unless their diffs are clearly unrelated:\n"+strings.Join(lines, "\n"))
	}
	userPrompt := strings.Join(planParts, "\n\n")

	raw, err := c.callLLM(ctx, systemPrompt, userPrompt, 8192, planMaxTokensCeiling)
	if err != nil {
		return nil, "", err
	}

	var result struct {
		Groups []planGroup `json:"groups"`
	}
	if err := unmarshalLLMJSON(raw, "groups", &result); err != nil {
		return nil, "", err
	}
	if len(expand) > 0 {
		for i := range result.Groups {
			result.Groups[i].Files = expandPlanFiles(result.Groups[i].Files, expand)
		}
	}
	return result.Groups, raw, nil
}

// expandPlanFiles replaces any "path/ (N files)" summary label the LLM
// echoed back with the real file paths it stands in for. Files the LLM
// returned verbatim (not a label) pass through unchanged. A label the LLM
// mangled or dropped simply has no match here — those files are not lost:
// commit_service's passthrough step sweeps any real file absent from every
// plan group into group 0.
func expandPlanFiles(files []string, expand map[string][]string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if real, ok := expand[f]; ok {
			out = append(out, real...)
			continue
		}
		out = append(out, f)
	}
	return out
}

// mergeFileListExpansion folds summary's label->files mapping into the
// shared expand map used across a whole planOnce call, and returns summary's
// labels for prompt rendering (possibly renamed — see below).
//
// SummarizeFileList guarantees unique labels *within* one call, but planOnce
// calls it twice (staged, then unstaged) and merges both results into one
// map. Two independent calls can legitimately produce the identical label
// text for two different sets of real files — e.g. staged and unstaged both
// happen to touch exactly 300 files under "vendor/lib/". Inserting blindly
// would let the second call's entry silently overwrite the first's in
// expand, and expandPlanFiles would then always resolve that label to the
// wrong file set. On collision, suffix the incoming label until it is
// unique, and reflect that renamed label back into what's rendered into the
// prompt so the LLM echoes the same string the caller can later resolve.
func mergeFileListExpansion(expand map[string][]string, summary commit.FileListSummary) []string {
	if len(summary.Expand) == 0 {
		return summary.Labels
	}
	rename := make(map[string]string, len(summary.Expand))
	for label, files := range summary.Expand {
		unique := label
		for n := 2; ; n++ {
			if _, taken := expand[unique]; !taken {
				break
			}
			unique = fmt.Sprintf("%s {%d}", label, n)
		}
		expand[unique] = files
		if unique != label {
			rename[label] = unique
		}
	}
	if len(rename) == 0 {
		return summary.Labels
	}
	labels := make([]string, len(summary.Labels))
	for i, l := range summary.Labels {
		if r, ok := rename[l]; ok {
			labels[i] = r
		} else {
			labels[i] = l
		}
	}
	return labels
}

func (c *Client) DetectTechnologies(ctx context.Context, req domainGitignore.DetectRequest) ([]string, error) {
	userPrompt := fmt.Sprintf("OS: %s\n\nTop-level directories:\n%s\n\nTracked files:\n%s",
		req.OS,
		strings.Join(req.Dirs, "\n"),
		strings.Join(req.Files, "\n"),
	)

	raw, err := c.callLLM(ctx, detectTechSystemPrompt, userPrompt, 1024, detectMaxTokensCeiling)
	if err != nil {
		return nil, err
	}

	var result struct {
		Technologies []string `json:"technologies"`
	}
	if err := unmarshalLLMJSON(raw, "technologies", &result); err != nil {
		return nil, err
	}
	if len(result.Technologies) == 0 {
		return nil, fmt.Errorf("LLM returned empty technologies\nraw: %s", extractJSON(raw))
	}

	return result.Technologies, nil
}

func (c *Client) GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string, existingScopes []project.Scope) ([]project.Scope, string, error) {
	userPrompt := fmt.Sprintf("Commit log (subject + changed files):\n%s\n\nTop-level directories:\n%s\n\nTracked files:\n%s",
		strings.Join(commits, "\n---\n"),
		strings.Join(dirs, "\n"),
		strings.Join(files, "\n"),
	)

	if len(existingScopes) > 0 {
		cfg := &project.Config{Scopes: existingScopes}
		userPrompt += "\n\nExisting scopes:\n- " + cfg.FormatScopesForLLM()
	}

	raw, err := c.callLLM(ctx, generateScopesSystemPrompt, userPrompt, 8192, scopesMaxTokensCeiling)
	if err != nil {
		return nil, "", err
	}

	var result struct {
		Scopes    []project.Scope `json:"scopes"`
		Reasoning string          `json:"reasoning"`
	}
	if err := unmarshalLLMJSON(raw, "scopes", &result); err != nil {
		return nil, "", err
	}
	// An empty scope list is a legitimate result for a fresh repository with
	// no commit history or tracked files to derive scopes from. Init should
	// still succeed and write an empty scopes list (which permits any scope).
	return result.Scopes, result.Reasoning, nil
}
