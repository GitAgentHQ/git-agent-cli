package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	goopenai "github.com/sashabaranov/go-openai"

	"github.com/fradser/git-agent/domain/commit"
	domainGitignore "github.com/fradser/git-agent/domain/gitignore"
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

// extractJSON finds the first {...} block in s, handling models that wrap JSON in prose.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return s
	}
	return s[start : end+1]
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

const generateSystemPrompt = `You are an expert software engineer. Generate a conventional commit message from the provided git diff. Respond ONLY with valid JSON in this exact format: {"title": "...", "body": "- bullet one\n- bullet two\n\nExplanation paragraph.", "outline": "..."}. Rules: title uses conventional commits format with one of these types: feat, fix, docs, style, refactor, perf, test, chore, build, ci, revert — ALL LOWERCASE ≤50 chars imperative mood; scope is optional, omit if no clear scope applies; body MUST start with one or more bullet points each on its own line beginning with "- " (hyphen space) then a blank line then a closing explanation paragraph — body text MUST use sentence case (first letter of each bullet and paragraph UPPERCASE), every line in body MUST be ≤72 characters (hard wrap if needed); outline is a human-readable summary of changes.`

const generateSystemPromptScoped = `You are an expert software engineer. Generate a conventional commit message from the provided git diff. Respond ONLY with valid JSON in this exact format: {"title": "...", "body": "- bullet one\n- bullet two\n\nExplanation paragraph.", "outline": "..."}. Rules: title uses conventional commits format with one of these types: feat, fix, docs, style, refactor, perf, test, chore, build, ci, revert — ALL LOWERCASE ≤50 chars imperative mood; REQUIRED scope — you MUST use one of the scopes listed in the user message (choose the most appropriate); body MUST start with one or more bullet points each on its own line beginning with "- " (hyphen space) then a blank line then a closing explanation paragraph — body text MUST use sentence case (first letter of each bullet and paragraph UPPERCASE), every line in body MUST be ≤72 characters (hard wrap if needed); outline is a human-readable summary of changes.`

const retrySystemPrompt = `You are an expert software engineer. Fix the commit message to satisfy the hook requirement. Respond ONLY with valid JSON: {"title": "...", "body": "- bullet one\n- bullet two\n\nExplanation paragraph.", "outline": "..."}. Title: conventional commits format ALL LOWERCASE ≤50 chars imperative mood. Body: MUST start with bullet points each beginning with "- " (hyphen space), then a blank line, then a closing explanation paragraph. Sentence case. Every line ≤72 chars.`

const planSystemPrompt = `You are an expert software engineer. Analyse the provided git diffs and split them into meaningful atomic commits.

If a PRIMARY DIRECTIVE is given, it is the most important constraint: only include files directly relevant to it; put those files in group 0; leave all unrelated files out.
If there are staged files and no PRIMARY DIRECTIVE, they MUST be group 0 (respect user intent).
Split remaining changes by logical concern (feature, bug fix, refactor, test, docs, etc.).
Each group should be a cohesive unit of change.

Respond ONLY with valid JSON:
{"groups": [{"files": ["..."], "title": "type(scope): description", "body": "- bullet\n\nexplanation", "outline": "human summary"}]}

Rules for title: conventional commits format, ALL LOWERCASE, ≤50 chars, imperative mood.
Scope is optional; omit if no clear scope applies.
Rules for body: bullet points then closing explanation paragraph — body text MUST use sentence case (first letter of each bullet and paragraph UPPERCASE), every line MUST be ≤72 characters (hard wrap long lines).`

const planSystemPromptScoped = `You are an expert software engineer. Analyse the provided git diffs and split them into meaningful atomic commits.

If a PRIMARY DIRECTIVE is given, it is the most important constraint: only include files directly relevant to it; put those files in group 0; leave all unrelated files out.
If there are staged files and no PRIMARY DIRECTIVE, they MUST be group 0 (respect user intent).
Split remaining changes by logical concern (feature, bug fix, refactor, test, docs, etc.).
Each group should be a cohesive unit of change.

Respond ONLY with valid JSON:
{"groups": [{"files": ["..."], "title": "type(scope): description", "body": "- bullet\n\nexplanation", "outline": "human summary"}]}

Rules for title: conventional commits format, ALL LOWERCASE, ≤50 chars, imperative mood.
REQUIRED scope — every title MUST use one of the scopes listed in the user message (choose the most appropriate per group). Files that map to different scopes MUST be placed in separate groups — never mix scopes within one group.
Rules for body: bullet points then closing explanation paragraph — body text MUST use sentence case (first letter of each bullet and paragraph UPPERCASE), every line MUST be ≤72 characters (hard wrap long lines).`

const detectTechSystemPrompt = `You are an expert software engineer. Analyze the project's OS, directories, and files to detect which technologies are used.

Return a JSON object with a "technologies" array containing only valid Toptal gitignore API identifiers.
Respond ONLY with valid JSON: {"technologies": ["go", "node", "visualstudiocode"]}

Rules:
- Include the OS identifier (e.g. "macos", "linux", "windows")
- Include programming languages detected from file extensions
- Include build tools, editors, and IDEs if evidence exists
- Use lowercase Toptal API identifiers only (e.g. "go", "node", "python", "rust", "jetbrains", "visualstudiocode")
- Do NOT include technologies with no evidence in the project files`

const generateScopesSystemPrompt = `You are an expert software engineer. Derive commit scopes from the top-level directories of the project, using commit history to validate and refine them.

Respond ONLY with valid JSON: {"scopes": ["..."], "reasoning": "..."}

Rules (STRICTLY enforce):
- Generate one scope per meaningful source directory listed in "Top-level directories"
- Skip dependency/build/generated directories (node_modules, vendor, dist, build, target, __pycache__, .next, out, coverage)
- Use the commit log (subject + changed files) to understand which directories represent distinct concerns and how they are named in practice
- Single-word directory names: use as-is or a well-known abbreviation (e.g. "cmd" -> "cli", "application" -> "app", "infrastructure" -> "infra")
- Hyphenated or compound names: use the distinguishing part or a well-known short form (e.g. "agentbook-skill" -> "skill", "my-frontend" -> "frontend")
- If commit history shows a consistent scope abbreviation for a directory, prefer that abbreviation
- NEVER invent scopes from file names or internal package names (e.g. do NOT derive "cs" from "commit_service.go")
- NEVER use commit types (feat, fix, chore, docs, refactor, test, style, perf) as scopes
- All scopes lowercase`

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
			promptParts = append(promptParts, "REQUIRED scopes (use the most appropriate one): "+strings.Join(req.Config.Scopes, ", "))
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

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    goopenai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature:         0,
		MaxCompletionTokens: 1024,
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
		planParts = append(planParts, "REQUIRED scopes (use the most appropriate one per group): "+strings.Join(req.Config.Scopes, ", "))
	}
	if req.StagedDiff != nil && len(req.StagedDiff.Files) > 0 {
		planParts = append(planParts, fmt.Sprintf("Staged diff (already staged by user — keep as group 0):\n<staged>\n%s\n</staged>\nStaged files: %s",
			req.StagedDiff.Content,
			strings.Join(req.StagedDiff.Files, ", "),
		))
	}
	if req.UnstagedDiff != nil && len(req.UnstagedDiff.Files) > 0 {
		planParts = append(planParts, fmt.Sprintf("Unstaged diff:\n<unstaged>\n%s\n</unstaged>\nUnstaged files: %s",
			req.UnstagedDiff.Content,
			strings.Join(req.UnstagedDiff.Files, ", "),
		))
	}
	userPrompt := strings.Join(planParts, "\n\n")

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: systemPrompt,
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
		return nil, fmt.Errorf("LLM returned empty response")
	}

	raw := extractJSON(resp.Choices[0].Message.Content)
	var result struct {
		Groups []struct {
			Files   []string `json:"files"`
			Title   string   `json:"title"`
			Body    string   `json:"body"`
			Outline string   `json:"outline"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parse response json: %w\nraw: %s", err, raw)
	}

	plan := &commit.CommitPlan{}
	for _, g := range result.Groups {
		plan.Groups = append(plan.Groups, commit.CommitGroup{
			Files: g.Files,
			Message: commit.CommitMessage{
				Title:   g.Title,
				Body:    g.Body,
				Outline: g.Outline,
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

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: detectTechSystemPrompt,
			},
			{
				Role:    goopenai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature:         0,
		MaxCompletionTokens: 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("openai chat completion: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	raw := extractJSON(resp.Choices[0].Message.Content)
	var result struct {
		Technologies []string `json:"technologies"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parse response json: %w\nraw: %s", err, raw)
	}

	return result.Technologies, nil
}

func (c *Client) GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string) ([]string, string, error) {
	userPrompt := fmt.Sprintf("Commit log (subject + changed files):\n%s\n\nTop-level directories:\n%s\n\nTracked files:\n%s",
		strings.Join(commits, "\n---\n"),
		strings.Join(dirs, "\n"),
		strings.Join(files, "\n"),
	)

	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model: c.model,
		Messages: []goopenai.ChatCompletionMessage{
			{
				Role:    goopenai.ChatMessageRoleSystem,
				Content: generateScopesSystemPrompt,
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
