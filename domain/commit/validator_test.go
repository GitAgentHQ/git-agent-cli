package commit_test

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/project"
)

func validMsg() string {
	return "feat: add user authentication\n\n- add login endpoint\n- add jwt token generation\n\nThis introduces basic authentication support.\n\nCo-Authored-By: Bot <bot@example.com>"
}

func TestValidateConventionalLanguageAware(t *testing.T) {
	chinese := "feat: 修复登录问题\n\n- 添加登录端点\n\n这个修复处理了登录失败。"
	if result := commit.ValidateConventionalWithLanguage(chinese, nil, "Chinese", ""); result.HasErrors() {
		t.Fatalf("explicit non-English message should pass: %v", result.Errors())
	}

	german := "feat: Ändere die Anmeldung\n\n- Füge den Login-Endpunkt hinzu\n\nDie Anmeldung unterstützt jetzt neue Tokens."
	if result := commit.ValidateConventionalWithLanguage(german, nil, "auto", "Ändere die Anmeldung"); result.HasErrors() {
		t.Fatalf("auto-detected non-English message should pass: %v", result.Errors())
	}

	english := "feat: Add login endpoint\n\n- add route handler\n\nThis adds the login route."
	for _, language := range []string{"", "auto", "English", "en", "en-US", "en-au", "en-ca"} {
		result := commit.ValidateConventionalWithLanguage(english, nil, language, "")
		if language == "English" || language == "en" || language == "en-US" || language == "en-au" || language == "en-ca" || language == "" || language == "auto" {
			if !result.HasErrors() {
				t.Errorf("language %q should retain English lowercase validation", language)
			}
		}
	}

	japanese := "feat: ログイン機能を追加して認証処理を改善します\n\n- ログイン処理を更新\n\n認証の動作を改善します。"
	if got := len([]rune(strings.Split(japanese, "\n")[0])); got > 50 {
		t.Fatalf("test title must be at most 50 runes, got %d", got)
	}
	if got := len(strings.Split(japanese, "\n")[0]); got <= 50 {
		t.Fatalf("test title must exceed 50 UTF-8 bytes, got %d", got)
	}
	if result := commit.ValidateConventionalWithLanguage(japanese, nil, "Japanese", ""); result.HasErrors() {
		t.Fatalf("non-English title length should count runes: %v", result.Errors())
	}
}

func TestValidateConventional(t *testing.T) {
	cases := []struct {
		name         string
		msg          string
		wantErrors   bool
		errContains  string
		warnContains string
	}{
		// --- passing ---
		{
			name:       "valid full message",
			msg:        validMsg(),
			wantErrors: false,
		},
		{
			name:       "valid with scope",
			msg:        "fix(auth): handle null token\n\n- return 401 on missing token\n- add unit test for null case\n\nA null token caused a panic; this makes the handler return 401.\n\nCo-Authored-By: Bot <bot@example.com>",
			wantErrors: false,
		},
		{
			name:       "valid breaking change bang",
			msg:        "feat(api)!: remove legacy endpoint\n\n- remove /v1/users endpoint\n- update client to use /v2/users\n\nThe v1 endpoint was deprecated in 2024 and is now removed.\n\nCo-Authored-By: Bot <bot@example.com>",
			wantErrors: false,
		},
		{
			name:       "valid without co-authored-by",
			msg:        "chore: update dependencies\n\n- bump go-openai from 1.40 to 1.41\n- bump cobra from 1.7 to 1.8\n\nRoutine dependency update to pick up bug fixes.",
			wantErrors: false,
		},
		{
			name:       "valid all lowercase scope with numbers",
			msg:        "fix(api2): handle timeout\n\n- add timeout handling\n\nThis prevents hangs on slow responses.",
			wantErrors: false,
		},

		// --- Rule 1: header format ---
		{
			name:        "missing type prefix",
			msg:         "add login feature\n\n- add route handler\n\nThis adds the login route.",
			wantErrors:  true,
			errContains: "header must match",
		},
		{
			name:       "invalid type",
			msg:        "feature: add login\n\n- add route handler\n\nThis adds the login route.",
			wantErrors: true,
		},
		{
			name:       "missing colon-space separator",
			msg:        "feat add login\n\n- add route handler\n\nThis adds the login route.",
			wantErrors: true,
		},

		// --- Rule 2: description lowercase ---
		{
			name:        "uppercase in description",
			msg:         "feat: Add login endpoint\n\n- add route handler\n- add session support\n\nThis adds the login route.",
			wantErrors:  true,
			errContains: "lowercase",
		},

		// --- Rule 3: title length ---
		{
			name:        "title exceeds 50 chars",
			msg:         "feat: add a very long title that exceeds fifty characters here\n\n- add route handler\n\nThis adds the route.",
			wantErrors:  true,
			errContains: "50 characters",
		},

		// --- Rule 4: trailing period ---
		{
			name:        "title ends with period",
			msg:         "feat: add login endpoint.\n\n- add route handler\n\nThis adds the login route.",
			wantErrors:  true,
			errContains: "period",
		},

		// --- body required ---
		{
			name:        "no body at all",
			msg:         "feat: add login endpoint",
			wantErrors:  true,
			errContains: "body is required",
		},
		{
			name:        "only header with blank line",
			msg:         "feat: add login endpoint\n",
			wantErrors:  true,
			errContains: "body is required",
		},

		// --- blank line between header and body ---
		{
			name:        "body not separated by blank line",
			msg:         "feat: add login endpoint\nbody text here\nmore text",
			wantErrors:  true,
			errContains: "blank line",
		},

		// --- Rule 6: bullet points required ---
		{
			name:        "body with no bullet points",
			msg:         "feat: add login endpoint\n\nJust some prose without bullets.\n\nMore prose here.",
			wantErrors:  true,
			errContains: "bullet point",
		},

		// --- Rule 7: body line length ---
		{
			name:         "body line exceeds 72 chars",
			msg:          "feat: add login endpoint\n\n- add route handler for the new login endpoint that is being introduced here\n\nThis adds the login route.",
			wantErrors:   false,
			warnContains: "72 characters",
		},
		{
			name:       "footer line allowed to exceed 72 chars",
			msg:        "feat: add login endpoint\n\n- add route handler\n\nThis adds the route.\n\nCo-Authored-By: A Very Long Name With Extra Parts <averylong+extra@subdomain.example.com>",
			wantErrors: false,
		},

		// --- Rule 8: explanation paragraph ---
		{
			name:        "no explanation after bullets",
			msg:         "feat: add login endpoint\n\n- add route handler\n- add session support",
			wantErrors:  true,
			errContains: "explanation paragraph",
		},
		{
			name:        "only footer after bullets",
			msg:         "feat: add login endpoint\n\n- add route handler\n\nCo-Authored-By: Bot <bot@example.com>",
			wantErrors:  true,
			errContains: "explanation paragraph",
		},

		// --- Rule 9: Co-Authored-By format ---
		// Git accepts trailers as free-form text, so any non-empty value is
		// valid — with or without an email.
		{
			name:       "co-authored-by with name only",
			msg:        "feat: add login endpoint\n\n- add route handler\n\nThis adds the login route.\n\nCo-Authored-By: OX Alpha",
			wantErrors: false,
		},
		{
			name:       "co-authored-by with bare email and no brackets",
			msg:        "feat: add login endpoint\n\n- add route handler\n\nThis adds the login route.\n\nCo-Authored-By: Bot bot@example.com",
			wantErrors: false,
		},
		{
			name:        "co-authored-by empty value",
			msg:         "feat: add login endpoint\n\n- add route handler\n\nThis adds the login route.\n\nCo-Authored-By:",
			wantErrors:  true,
			errContains: "Co-Authored-By",
		},

		// --- Warning W1: past-tense in description ---
		{
			name:         "description past-tense verb",
			msg:          "feat: added user authentication\n\n- add login endpoint\n\nThis introduces authentication support.",
			wantErrors:   false,
			warnContains: "past-tense",
		},

		// --- Warning W2: past-tense in bullet ---
		{
			name:         "bullet past-tense verb",
			msg:          "feat: add user authentication\n\n- added login endpoint\n\nThis introduces authentication support.",
			wantErrors:   false,
			warnContains: "past-tense",
		},

		// --- generic trailers: 72-char exemption ---
		{
			name:       "signed-off-by allowed to exceed 72 chars",
			msg:        "feat: add login endpoint\n\n- add route handler\n\nThis adds the route.\n\nSigned-off-by: A Very Long Name With Extra Detail <longname@subdomain.example.com>",
			wantErrors: false,
		},
		{
			name:       "reviewed-by allowed to exceed 72 chars",
			msg:        "feat: add login endpoint\n\n- add route handler\n\nThis adds the route.\n\nReviewed-by: Another Long Reviewer Name <reviewer@subdomain.example.com>",
			wantErrors: false,
		},
		{
			name:       "custom trailer allowed to exceed 72 chars",
			msg:        "feat: add login endpoint\n\n- add route handler\n\nThis adds the route.\n\nX-Custom-Trailer: some-very-long-value-that-exceeds-seventy-two-characters-no-error",
			wantErrors: false,
		},

		// --- generic trailers: explanation paragraph logic ---
		{
			name:        "only signed-off-by after bullets is not explanation",
			msg:         "feat: add login endpoint\n\n- add route handler\n\nSigned-off-by: Bob <bob@example.com>",
			wantErrors:  true,
			errContains: "explanation paragraph",
		},
		{
			name:       "explanation present before signed-off-by",
			msg:        "feat: add login endpoint\n\n- add route handler\n\nThis adds the login route.\n\nSigned-off-by: Bob <bob@example.com>",
			wantErrors: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := commit.ValidateConventional(tc.msg, nil)

			if tc.wantErrors && !result.HasErrors() {
				t.Errorf("expected errors but got none")
			}
			if !tc.wantErrors && result.HasErrors() {
				t.Errorf("expected no errors but got: %v", result.Errors())
			}
			if tc.errContains != "" {
				found := false
				for _, e := range result.Errors() {
					if strings.Contains(e, tc.errContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("errors %v do not contain %q", result.Errors(), tc.errContains)
				}
			}
			if tc.warnContains != "" {
				found := false
				for _, w := range result.Warnings() {
					if strings.Contains(w, tc.warnContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("warnings %v do not contain %q", result.Warnings(), tc.warnContains)
				}
			}
		})
	}
}

func TestValidateModelCoAuthor(t *testing.T) {
	const baseBody = "feat: add login endpoint\n\n- add route handler\n\nThis adds the login route.\n\n"

	// Source of truth: domain/project.DefaultModelCoAuthorDomains. Referencing it
	// here keeps the tests in lockstep with the production allow-list instead of
	// drifting as providers are added.
	defaults := project.DefaultModelCoAuthorDomains

	cases := []struct {
		name        string
		msg         string
		domains     []string
		wantErrors  bool
		errContains string
	}{
		{
			name:       "anthropic trailer alongside git agent passes",
			msg:        baseBody + "Co-Authored-By: Git Agent <noreply@git-agent.dev>\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
			domains:    defaults,
			wantErrors: false,
		},
		{
			name:       "openai trailer alone passes",
			msg:        baseBody + "Co-Authored-By: GPT-5 <noreply@openai.com>",
			domains:    defaults,
			wantErrors: false,
		},
		{
			name:       "google trailer alone passes",
			msg:        baseBody + "Co-Authored-By: Gemini Pro <noreply@google.com>",
			domains:    defaults,
			wantErrors: false,
		},
		{
			name:       "case-insensitive domain match",
			msg:        baseBody + "Co-Authored-By: Claude Opus 4.6 <noreply@ANTHROPIC.COM>",
			domains:    []string{"anthropic.com"},
			wantErrors: false,
		},
		{
			name:       "case-insensitive allow-list entry",
			msg:        baseBody + "Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
			domains:    []string{"Anthropic.COM"},
			wantErrors: false,
		},
		{
			name:       "user-extended domain passes",
			msg:        baseBody + "Co-Authored-By: Acme Bot <bot@acme.ai>",
			domains:    append([]string{"acme.ai"}, defaults...),
			wantErrors: false,
		},
		{
			name:        "only git agent trailer is rejected",
			msg:         baseBody + "Co-Authored-By: Git Agent <noreply@git-agent.dev>",
			domains:     defaults,
			wantErrors:  true,
			errContains: "Co-Authored-By trailer from one of",
		},
		{
			name:        "no co-authored-by at all is rejected",
			msg:         "feat: x\n\n- y\n\nz.",
			domains:     defaults,
			wantErrors:  true,
			errContains: "Co-Authored-By trailer from one of",
		},
		{
			name:        "human co-author with non-listed domain rejected",
			msg:         baseBody + "Co-Authored-By: Alice <alice@example.com>",
			domains:     defaults,
			wantErrors:  true,
			errContains: "Co-Authored-By trailer from one of",
		},
		{
			name:       "malformed co-authored-by line ignored, sibling valid trailer accepted",
			msg:        baseBody + "Co-Authored-By: Bot bot@anthropic.com\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
			domains:    defaults,
			wantErrors: false,
		},
		{
			name:       "malformed co-authored-by line alone is rejected",
			msg:        baseBody + "Co-Authored-By: Bot bot@anthropic.com",
			domains:    defaults,
			wantErrors: true,
		},
		{
			name:       "empty allow-list rejects every commit",
			msg:        baseBody + "Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
			domains:    nil,
			wantErrors: true,
		},
		{
			name:       "whitespace-only allow-list entries are dropped",
			msg:        baseBody + "Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
			domains:    []string{"  ", "", "anthropic.com"},
			wantErrors: false,
		},
		{
			name:       "subdomain does not satisfy parent domain entry",
			msg:        baseBody + "Co-Authored-By: Bot <bot@api.anthropic.com>",
			domains:    []string{"anthropic.com"},
			wantErrors: true,
		},
		{
			name:       "multiple trailers — any single match passes",
			msg:        baseBody + "Co-Authored-By: Alice <alice@example.com>\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>\nCo-Authored-By: Git Agent <noreply@git-agent.dev>",
			domains:    defaults,
			wantErrors: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := commit.ValidateModelCoAuthor(tc.msg, tc.domains)

			if tc.wantErrors && !result.HasErrors() {
				t.Errorf("expected errors but got none")
			}
			if !tc.wantErrors && result.HasErrors() {
				t.Errorf("expected no errors but got: %v", result.Errors())
			}
			if tc.errContains != "" {
				found := false
				for _, e := range result.Errors() {
					if strings.Contains(e, tc.errContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("errors %v do not contain %q", result.Errors(), tc.errContains)
				}
			}
		})
	}

	// Every built-in default domain must pass on its own — generated from
	// project.DefaultModelCoAuthorDomains so adding a provider cannot leave it
	// untested.
	builtInModels := map[string]string{
		"anthropic.com":        "Claude Opus 4.6",
		"openai.com":           "GPT-5",
		"google.com":           "Gemini Pro",
		"x.ai":                 "Grok 4.5",
		"zhipuai.cn":           "GLM-4.5",
		"qwen.ai":              "Qwen3",
		"deepseek.com":         "DeepSeek V3",
		"moonshot.ai":          "Kimi K2",
		"models.git-agent.dev": "Ox Alpha",
	}
	for _, domain := range project.DefaultModelCoAuthorDomains {
		domain := domain
		t.Run("built-in/"+domain, func(t *testing.T) {
			msg := baseBody + "Co-Authored-By: " + builtInModels[domain] + " <noreply@" + domain + ">"
			result := commit.ValidateModelCoAuthor(msg, project.DefaultModelCoAuthorDomains)
			if result.HasErrors() {
				t.Errorf("built-in domain %s should pass with the default allow-list, got: %v",
					domain, result.Errors())
			}
		})
	}
}

func TestHasModelCoAuthor(t *testing.T) {
	defaults := project.DefaultModelCoAuthorDomains

	cases := []struct {
		name     string
		trailers []commit.Trailer
		domains  []string
		want     bool
	}{
		{
			name:     "matching anthropic trailer",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Claude Opus 4.7 <noreply@anthropic.com>"}},
			domains:  defaults,
			want:     true,
		},
		{
			name:     "matching built-in grok trailer",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Grok 4.5 <noreply@x.ai>"}},
			domains:  defaults,
			want:     true,
		},
		{
			name: "git agent trailer alone is not enough",
			trailers: []commit.Trailer{
				{Key: "Co-Authored-By", Value: "Git Agent <noreply@git-agent.dev>"},
			},
			domains: defaults,
			want:    false,
		},
		{
			name: "matching trailer alongside git agent",
			trailers: []commit.Trailer{
				{Key: "Co-Authored-By", Value: "Git Agent <noreply@git-agent.dev>"},
				{Key: "Co-Authored-By", Value: "Claude Opus 4.7 <noreply@anthropic.com>"},
			},
			domains: defaults,
			want:    true,
		},
		{
			name:     "case-insensitive Key (co-authored-by)",
			trailers: []commit.Trailer{{Key: "co-authored-by", Value: "Claude <noreply@anthropic.com>"}},
			domains:  defaults,
			want:     true,
		},
		{
			name:     "case-insensitive domain in value",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Claude <noreply@ANTHROPIC.COM>"}},
			domains:  []string{"anthropic.com"},
			want:     true,
		},
		{
			name:     "user-extended domain",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Acme Bot <bot@acme.ai>"}},
			domains:  append([]string{"acme.ai"}, defaults...),
			want:     true,
		},
		{
			name:     "non-allow-listed domain",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Alice <alice@example.com>"}},
			domains:  defaults,
			want:     false,
		},
		{
			name:     "non-co-author trailer ignored",
			trailers: []commit.Trailer{{Key: "Signed-off-by", Value: "Bob <bob@anthropic.com>"}},
			domains:  defaults,
			want:     false,
		},
		{
			name:     "value missing email returns false",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Claude"}},
			domains:  defaults,
			want:     false,
		},
		{
			name:     "empty trailers returns false",
			trailers: nil,
			domains:  defaults,
			want:     false,
		},
		{
			name:     "empty allow-list returns false even with matching trailer",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Claude <noreply@anthropic.com>"}},
			domains:  nil,
			want:     false,
		},
		{
			name:     "subdomain does not satisfy parent entry",
			trailers: []commit.Trailer{{Key: "Co-Authored-By", Value: "Bot <bot@api.anthropic.com>"}},
			domains:  []string{"anthropic.com"},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commit.HasModelCoAuthor(tc.trailers, tc.domains); got != tc.want {
				t.Errorf("HasModelCoAuthor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateConventional_ScopeWhitelist(t *testing.T) {
	allowed := []string{"app", "cli", "infra"}
	base := "\n\n- add route handler\n\nThis adds the route."

	cases := []struct {
		name        string
		msg         string
		scopes      []string
		wantErrors  bool
		errContains string
	}{
		{
			name:       "allowed scope passes",
			msg:        "feat(app): add login" + base,
			scopes:     allowed,
			wantErrors: false,
		},
		{
			name:        "disallowed scope blocked",
			msg:         "docs(code-graph-design): restructure" + base,
			scopes:      allowed,
			wantErrors:  true,
			errContains: "not in the allowed list",
		},
		{
			name:       "no scope passes when scopes configured",
			msg:        "feat: add login" + base,
			scopes:     allowed,
			wantErrors: false,
		},
		{
			name:       "any scope passes when no scopes configured",
			msg:        "feat(anything): add login" + base,
			scopes:     nil,
			wantErrors: false,
		},
		{
			name:       "any scope passes with empty scopes slice",
			msg:        "feat(anything): add login" + base,
			scopes:     []string{},
			wantErrors: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := commit.ValidateConventional(tc.msg, tc.scopes)

			if tc.wantErrors && !result.HasErrors() {
				t.Errorf("expected errors but got none")
			}
			if !tc.wantErrors && result.HasErrors() {
				t.Errorf("expected no errors but got: %v", result.Errors())
			}
			if tc.errContains != "" {
				found := false
				for _, e := range result.Errors() {
					if strings.Contains(e, tc.errContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("errors %v do not contain %q", result.Errors(), tc.errContains)
				}
			}
		})
	}
}

func TestInferModelCoAuthor(t *testing.T) {
	cases := []struct {
		modelID string
		wantKey string
		wantVal string
		wantOk  bool
	}{
		{
			modelID: "gemini-3.6-flash-high",
			wantKey: "Co-Authored-By",
			wantVal: "Gemini 3.6 Flash <noreply@google.com>",
			wantOk:  true,
		},
		{
			modelID: "opencode/deepseek-v4-pro",
			wantKey: "Co-Authored-By",
			wantVal: "DeepSeek V4 Pro <noreply@deepseek.com>",
			wantOk:  true,
		},
		{
			modelID: "claude-3-5-sonnet-20241022",
			wantKey: "Co-Authored-By",
			wantVal: "Claude 3.5 Sonnet <noreply@anthropic.com>",
			wantOk:  true,
		},
		{
			modelID: "claude-opus-4-6-thinking",
			wantKey: "Co-Authored-By",
			wantVal: "Claude Opus 4.6 <noreply@anthropic.com>",
			wantOk:  true,
		},
		{
			modelID: "gpt-5.6-luna",
			wantKey: "Co-Authored-By",
			wantVal: "GPT 5.6 Luna <noreply@openai.com>",
			wantOk:  true,
		},
		{
			modelID: "bailian/qwen3.8-max",
			wantKey: "Co-Authored-By",
			wantVal: "Qwen 3.8 Max <noreply@qwen.ai>",
			wantOk:  true,
		},
		{
			modelID: "ark/glm-5-2",
			wantKey: "Co-Authored-By",
			wantVal: "GLM 5.2 <noreply@zhipuai.cn>",
			wantOk:  true,
		},
		{
			modelID: "kimi-k3",
			wantKey: "Co-Authored-By",
			wantVal: "Kimi K3 <noreply@moonshot.ai>",
			wantOk:  true,
		},
		{
			modelID: "grok-4.5",
			wantKey: "Co-Authored-By",
			wantVal: "Grok 4.5 <noreply@x.ai>",
			wantOk:  true,
		},
		{
			modelID: "grok-4.20-0309-non-reasoning",
			wantKey: "Co-Authored-By",
			wantVal: "Grok 4.20 <noreply@x.ai>",
			wantOk:  true,
		},
		{
			modelID: "openrouter/stealth/ox-alpha",
			wantKey: "Co-Authored-By",
			wantVal: "Ox Alpha <noreply@models.git-agent.dev>",
			wantOk:  true,
		},
		{
			modelID: "ox-alpha-free",
			wantKey: "Co-Authored-By",
			wantVal: "Ox Alpha <noreply@models.git-agent.dev>",
			wantOk:  true,
		},
		{
			modelID: "unknown-custom-model",
			wantKey: "Co-Authored-By",
			wantVal: "Unknown Custom Model <noreply@models.git-agent.dev>",
			wantOk:  true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.modelID, func(t *testing.T) {
			got, ok := commit.InferModelCoAuthor(tc.modelID)
			if ok != tc.wantOk {
				t.Fatalf("InferModelCoAuthor(%q) ok = %v, want %v", tc.modelID, ok, tc.wantOk)
			}
			if ok {
				if got.Key != tc.wantKey || got.Value != tc.wantVal {
					t.Errorf("InferModelCoAuthor(%q) = {%q, %q}, want {%q, %q}",
						tc.modelID, got.Key, got.Value, tc.wantKey, tc.wantVal)
				}
			}
		})
	}
}
