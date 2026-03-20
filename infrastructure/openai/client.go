package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	goopenai "github.com/sashabaranov/go-openai"

	"github.com/fradser/ga-cli/domain/commit"
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

func truncateLines(s string, max int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	return strings.Join(lines[:max], "\n")
}

// extractJSON finds the first {...} block in s, handling models that wrap JSON in prose.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return s
	}
	return s[start : end+1]
}

func (c *Client) Generate(ctx context.Context, req commit.GenerateRequest) (*commit.CommitMessage, error) {
	content := truncateLines(req.Diff.Content, 500)

	userPrompt := fmt.Sprintf("Git diff:\n<diff>\n%s\n</diff>\n\nStaged files: %s",
		content,
		strings.Join(req.Diff.Files, ", "),
	)
	if req.Intent != "" {
		userPrompt += "\n\nUser intent: " + req.Intent
	}
	if req.Config != nil && len(req.Config.Scopes) > 0 {
		userPrompt += "\n\nValid scopes: " + strings.Join(req.Config.Scopes, ", ")
	}
	if req.HookFeedback != "" {
		userPrompt += "\n\nPrevious attempt was rejected by the commit hook. Reason:\n" + req.HookFeedback + "\nFix the commit message to satisfy the requirement above."
	}

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: `You are an expert software engineer. Generate a conventional commit message from the provided git diff. Respond ONLY with valid JSON in this exact format: {"title": "...", "body": "...", "outline": "..."}. Rules: title uses conventional commits format with one of these types: feat, fix, docs, style, refactor, perf, test, chore, build, ci, revert — ALL LOWERCASE ≤50 chars imperative mood; body has bullet points then explanation paragraph; outline is a human-readable summary of changes.`,
			},
			{
				Role:    goopenai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature:         0,
		MaxCompletionTokens: 32768,
	})
	if err != nil {
		return nil, fmt.Errorf("openai chat completion: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("LLM returned empty response (choices=%d, finish_reason=%s)",
			len(resp.Choices), func() string {
				if len(resp.Choices) > 0 {
					return string(resp.Choices[0].FinishReason)
				}
				return "n/a"
			}())
	}

	raw := extractJSON(resp.Choices[0].Message.Content)
	var result struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Outline string `json:"outline"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parse response json: %w\nraw: %s", err, raw)
	}

	return &commit.CommitMessage{
		Title:   result.Title,
		Body:    result.Body,
		Outline: result.Outline,
	}, nil
}

func (c *Client) GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string) ([]string, string, error) {
	userPrompt := fmt.Sprintf("Recent commits:\n%s\n\nTop-level directories:\n%s\n\nTracked files:\n%s",
		strings.Join(commits, "\n"),
		strings.Join(dirs, "\n"),
		strings.Join(files, "\n"),
	)

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: `You are an expert software engineer. Derive commit scopes strictly from the top-level directories of the project.

Respond ONLY with valid JSON: {"scopes": ["..."], "reasoning": "..."}

Rules (STRICTLY enforce):
- Each scope MUST correspond to an actual top-level directory listed in "Top-level directories"
- Use commit history and tracked files only to understand intent, NOT to invent extra scopes
- Single-word directory names: use as-is (e.g. "cmd" → "cli" if it holds CLI code, "pkg", "docs", "domain", "hooks")
- Multi-word or long names: abbreviate to a well-known short form (e.g. "application" → "app", "infrastructure" → "infra")
- NEVER invent scopes from file names or internal package names (e.g. do NOT derive "cs" from "commit_service.go")
- NEVER use commit types (feat, fix, chore, docs, refactor, test, style, perf) as scopes
- All scopes lowercase, no hyphens`,
			},
			{
				Role:    goopenai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature:         0,
		MaxCompletionTokens: 32768,
	})
	if err != nil {
		return nil, "", fmt.Errorf("openai chat completion: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, "", fmt.Errorf("LLM returned empty response (choices=%d, finish_reason=%s)",
			len(resp.Choices), func() string {
				if len(resp.Choices) > 0 {
					return string(resp.Choices[0].FinishReason)
				}
				return "n/a"
			}())
	}

	raw := extractJSON(resp.Choices[0].Message.Content)
	var result struct {
		Scopes    []string `json:"scopes"`
		Reasoning string   `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, "", fmt.Errorf("parse response json: %w", err)
	}

	return result.Scopes, result.Reasoning, nil
}
