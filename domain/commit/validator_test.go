package commit_test

import (
	"strings"
	"testing"

	"github.com/fradser/git-agent/domain/commit"
)

func validMsg() string {
	return "feat: add user authentication\n\n- add login endpoint\n- add jwt token generation\n\nThis introduces basic authentication support.\n\nCo-Authored-By: Bot <bot@example.com>"
}

func TestValidateConventional(t *testing.T) {
	cases := []struct {
		name        string
		msg         string
		wantErrors  bool
		errContains string
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
			name:        "body line exceeds 72 chars",
			msg:         "feat: add login endpoint\n\n- add route handler for the new login endpoint that is being introduced here\n\nThis adds the login route.",
			wantErrors:  true,
			errContains: "72 characters",
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := commit.ValidateConventional(tc.msg)

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
