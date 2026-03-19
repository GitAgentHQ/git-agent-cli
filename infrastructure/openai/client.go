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

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: `You are an expert software engineer. Generate a conventional commit message from the provided git diff. Respond ONLY with valid JSON in this exact format: {"title": "...", "body": "...", "outline": "..."}. Rules: title is conventional commits format ALL LOWERCASE ≤50 chars imperative mood; body has bullet points then explanation paragraph; outline is a human-readable summary of changes.`,
			},
			{
				Role:    goopenai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature: 0,
		MaxTokens:   800,
	})
	if err != nil {
		return nil, fmt.Errorf("openai chat completion: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("LLM returned empty response")
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

func (c *Client) GenerateScopes(ctx context.Context, commits []string, dirs []string) ([]string, string, error) {
	userPrompt := fmt.Sprintf("Recent commits:\n%s\n\nDirectories:\n%s",
		strings.Join(commits, "\n"),
		strings.Join(dirs, "\n"),
	)

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: `You are an expert software engineer. Analyze git commit history and directory structure to suggest commit scopes. Respond ONLY with valid JSON: {"scopes": ["..."], "reasoning": "..."}. Suggest 3-8 short lowercase scope identifiers.`,
			},
			{
				Role:    goopenai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature: 0,
		MaxTokens:   400,
	})
	if err != nil {
		return nil, "", fmt.Errorf("openai chat completion: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, "", fmt.Errorf("LLM returned empty response")
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
