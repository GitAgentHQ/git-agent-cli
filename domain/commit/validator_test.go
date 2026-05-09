package commit_test

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/commit"
)

func validMsg() string {
	return "feat: add user authentication\n\n- add login endpoint\n- add jwt token generation\n\nThis introduces basic authentication support.\n\nCo-Authored-By: Bot <bot@example.com>"
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
		{
			name:        "co-authored-by missing angle brackets",
			msg:         "feat: add login endpoint\n\n- add route handler\n\nThis adds the login route.\n\nCo-Authored-By: Bot bot@example.com",
			wantErrors:  true,
			errContains: "Co-Authored-By",
		},
		{
			name:        "co-authored-by missing email",
			msg:         "feat: add login endpoint\n\n- add route handler\n\nThis adds the login route.\n\nCo-Authored-By: Bot",
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

	defaults := []string{"anthropic.com", "openai.com", "google.com"}

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
