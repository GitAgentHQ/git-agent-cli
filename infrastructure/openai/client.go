package openai

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"

	goopenai "github.com/sashabaranov/go-openai"

	"github.com/gitagenthq/git-agent/domain/commit"
	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
	"github.com/gitagenthq/git-agent/domain/project"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

type Client struct {
	inner *goopenai.Client
	model string
}

func NewClient(apiKey, baseURL, model string) *Client {
	cfg := goopenai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &Client{
		inner: goopenai.NewClientWithConfig(cfg),
		model: model,
	}
}

// isReasoningModel reports whether the model name indicates an OpenAI o-series
// reasoning model that accepts reasoning_effort but rejects temperature.
func isReasoningModel(model string) bool {
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if model == prefix || strings.HasPrefix(model, prefix+"-") || strings.HasPrefix(model, prefix+"/") {
			return true
		}
	}
	return false
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

const generateSystemPromptScoped = `You are an expert software engineer. Generate a conventional commit message from the provided git diff. Respond ONLY with valid JSON in this exact format: {"title": "...", "bullets": ["Bullet one", "Bullet two"], "explanation": "Explanation paragraph."}. Rules: title uses conventional commits format with one of these types: feat, fix, docs, style, refactor, perf, test, chore, build, ci, revert — ALL LOWERCASE ≤50 chars imperative mood; REQUIRED scope — you MUST use one of the scopes listed in the user message (choose the most appropriate); bullets is an array of strings each starting with an UPPERCASE first letter, imperative mood, targeting ≤72 chars per entry; explanation is a closing paragraph in sentence case; all text targets ≤72 characters per line.`

const retrySystemPrompt = `You are an expert software engineer. Fix the commit message to satisfy the hook requirement. Respond ONLY with valid JSON: {"title": "...", "bullets": ["Bullet one", "Bullet two"], "explanation": "Explanation paragraph."}. Title: conventional commits format ALL LOWERCASE ≤50 chars imperative mood. Bullets: array of strings each starting with UPPERCASE first letter, imperative mood, ≤72 chars per entry. Explanation: closing paragraph, sentence case. All text targets ≤72 characters per line.`

const planSystemPrompt = `You are an expert software engineer. Analyse the provided file paths and split them into meaningful atomic commits.

If a PRIMARY DIRECTIVE is given, it is the most important constraint: only include files directly relevant to it; put those files in group 0; leave all unrelated files out.
If there are staged files and no PRIMARY DIRECTIVE, they MUST be group 0 (respect user intent).
Split remaining changes by logical concern (feature, bug fix, refactor, test, docs, etc.) — infer the nature of each change from the file path, name, and directory structure.
Each group should be a cohesive unit of change.

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

Respond ONLY with valid JSON:
{"groups": [{"files": ["..."], "title": "type(scope): description", "bullets": ["Bullet one"], "explanation": "Explanation."}]}

Rules for title: conventional commits format, ALL LOWERCASE, ≤50 chars, imperative mood.
REQUIRED scope — every title MUST use one of the scopes listed in the user message (choose the most appropriate per group). Files that map to different scopes MUST be placed in separate groups — never mix scopes within one group.
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
- Generate one scope per meaningful source directory listed in "Top-level directories"
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
- Each scope MUST have a "description" field: a concise phrase (under 15 words) explaining what the scope covers, so AI can choose the right scope when generating commit messages`

// callLLM sends a chat completion request with retry logic for transient failures and empty responses.
func (c *Client) callLLM(ctx context.Context, system, user string, maxTokens int) (string, error) {
	const maxAttempts = 3

	msgs := []goopenai.ChatCompletionMessage{
		{Role: goopenai.ChatMessageRoleSystem, Content: system},
		{Role: goopenai.ChatMessageRoleUser, Content: user},
	}

	req := goopenai.ChatCompletionRequest{
		Model:               c.model,
		Messages:            msgs,
		MaxCompletionTokens: maxTokens,
	}

	if isReasoningModel(c.model) {
		req.ReasoningEffort = "low"
	} else {
		req.Temperature = 0
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := c.inner.CreateChatCompletion(ctx, req)
		if err != nil {
			// Check for non-transient API errors that should not be retried.
			if apiErr := classifyAPIError(err); apiErr != nil {
				return "", apiErr
			}
			lastErr = fmt.Errorf("openai chat completion: %w", err)
			continue
		}

		if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
			// When the model hit the token limit (finish_reason=length) without
			// producing content — common with reasoning models that spend tokens
			// on chain-of-thought — double the budget for the next attempt.
			if len(resp.Choices) > 0 && resp.Choices[0].FinishReason == goopenai.FinishReasonLength {
				req.MaxCompletionTokens *= 2
				lastErr = fmt.Errorf("LLM exhausted token limit without producing content (model=%s, max_tokens=%d, attempt=%d/%d)",
					c.model, req.MaxCompletionTokens/2, attempt+1, maxAttempts)
				continue
			}
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
			promptParts = append(promptParts, "REQUIRED scopes (use the most appropriate one):\n- "+req.Config.FormatScopesForLLM())
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
		raw, err := c.callLLM(ctx, systemPrompt, userPrompt, 4096)
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

	var systemPrompt string
	if hasScopes {
		systemPrompt = planSystemPromptScoped
	} else {
		systemPrompt = planSystemPrompt
	}

	var planParts []string
	if req.Intent != "" {
		planParts = append(planParts, "PRIMARY DIRECTIVE — focus only on this: "+req.Intent)
	}
	if hasScopes {
		planParts = append(planParts, "REQUIRED scopes (use the most appropriate one per group):\n- "+req.Config.FormatScopesForLLM())
	}
	if req.StagedDiff != nil && len(req.StagedDiff.Files) > 0 {
		planParts = append(planParts, fmt.Sprintf("Staged files (already staged by user — keep as group 0):\n%s",
			strings.Join(req.StagedDiff.Files, "\n"),
		))
	}
	if req.UnstagedDiff != nil && len(req.UnstagedDiff.Files) > 0 {
		planParts = append(planParts, fmt.Sprintf("Unstaged files:\n%s",
			strings.Join(req.UnstagedDiff.Files, "\n"),
		))
	}
	userPrompt := strings.Join(planParts, "\n\n")

	raw, err := c.callLLM(ctx, systemPrompt, userPrompt, 8192)
	if err != nil {
		return nil, err
	}

	var result struct {
		Groups []struct {
			Files       []string `json:"files"`
			Title       string   `json:"title"`
			Bullets     []string `json:"bullets"`
			Explanation string   `json:"explanation"`
		} `json:"groups"`
	}
	if err := unmarshalLLMJSON(raw, "groups", &result); err != nil {
		return nil, err
	}
	if len(result.Groups) == 0 {
		return nil, fmt.Errorf("LLM returned empty plan (no commit groups)\nraw: %s", extractJSON(raw))
	}

	plan := &commit.CommitPlan{}
	for _, g := range result.Groups {
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

func (c *Client) DetectTechnologies(ctx context.Context, req domainGitignore.DetectRequest) ([]string, error) {
	userPrompt := fmt.Sprintf("OS: %s\n\nTop-level directories:\n%s\n\nTracked files:\n%s",
		req.OS,
		strings.Join(req.Dirs, "\n"),
		strings.Join(req.Files, "\n"),
	)

	raw, err := c.callLLM(ctx, detectTechSystemPrompt, userPrompt, 1024)
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

func (c *Client) GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string) ([]project.Scope, string, error) {
	userPrompt := fmt.Sprintf("Commit log (subject + changed files):\n%s\n\nTop-level directories:\n%s\n\nTracked files:\n%s",
		strings.Join(commits, "\n---\n"),
		strings.Join(dirs, "\n"),
		strings.Join(files, "\n"),
	)

	raw, err := c.callLLM(ctx, generateScopesSystemPrompt, userPrompt, 8192)
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
	if len(result.Scopes) == 0 {
		return nil, "", fmt.Errorf("LLM returned empty scopes\nraw: %s", extractJSON(raw))
	}
	return result.Scopes, result.Reasoning, nil
}
