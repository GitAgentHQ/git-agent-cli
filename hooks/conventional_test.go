package hooks_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/hooks"
)

func TestConventionalHook(t *testing.T) {
	// Write embedded script to a temp file
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "conventional.sh")
	if err := os.WriteFile(scriptPath, hooks.Conventional, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	type payload struct {
		CommitMessage string   `json:"commitMessage"`
		Diff          string   `json:"diff"`
		Intent        string   `json:"intent"`
		StagedFiles   []string `json:"stagedFiles"`
		Config        struct {
			Scopes   []string `json:"scopes"`
			Language string   `json:"language"`
		} `json:"config"`
	}

	makeInputWithLanguage := func(msg, language, intent string) string {
		p := payload{
			CommitMessage: msg,
			Intent:        intent,
			StagedFiles:   []string{},
		}
		p.Config.Language = language
		b, _ := json.Marshal(p)
		return string(b)
	}
	makeInput := func(msg string) string {
		return makeInputWithLanguage(msg, "", "")
	}

	fullMsg := func(header, bullets, explanation string) string {
		return header + "\n\n" + bullets + "\n\n" + explanation
	}

	cases := []struct {
		name     string
		msg      string
		wantExit int
		wantErr  string
	}{
		// --- passing ---
		{
			"valid full message",
			"feat: add user authentication\n\n- add login endpoint\n- add jwt token generation\n\nThis introduces basic authentication support.\n\nCo-Authored-By: Bot <bot@example.com>",
			0, "",
		},
		{
			"scoped fix with full body",
			"fix(auth): handle null token\n\n- return 401 on missing token\n- add test for null case\n\nA null token caused a panic; this makes the handler return 401.\n\nCo-Authored-By: Bot <bot@example.com>",
			0, "",
		},
		{
			"breaking bang",
			"feat!: drop support for go 1.20\n\n- remove go 1.20 build tag\n- update ci matrix\n\nGo 1.20 is EOL and no longer supported.\n\nCo-Authored-By: Bot <bot@example.com>",
			0, "",
		},
		{
			"breaking bang scoped",
			"feat(api)!: remove legacy endpoint\n\n- remove /v1/users route\n- update api docs\n\nThe v1 endpoint was deprecated and is now removed.\n\nCo-Authored-By: Bot <bot@example.com>",
			0, "",
		},
		{
			"without co-authored-by",
			fullMsg("chore: update dependencies", "- bump go-openai to 1.41\n- bump cobra to 1.8", "Routine dependency update to pick up bug fixes."),
			0, "",
		},
		{
			"name-only co-authored-by",
			"feat: add user authentication\n\n- add login endpoint\n\nThis introduces basic authentication support.\n\nCo-Authored-By: Ox Alpha",
			0, "",
		},
		{
			"no co-authored-by footer",
			fullMsg("feat: add login endpoint", "- add route handler", "This adds the login route."),
			0, "",
		},
		{
			"body with BREAKING CHANGE footer",
			"fix: handle edge case\n\n- add null check\n\nThis prevents panics.\n\nBREAKING CHANGE: removed fallback",
			0, "",
		},
		{
			"body with BREAKING-CHANGE footer",
			"fix: handle edge case\n\n- add null check\n\nThis prevents panics.\n\nBREAKING-CHANGE: removed fallback",
			0, "",
		},

		// --- error: header format ---
		{"missing type", "add login feature", 1, ""},
		{"missing colon space", "feat add login", 1, ""},
		{"empty description", "feat:", 1, ""},
		{"invalid type", "feature: add login", 1, ""},

		// --- error: body not separated by blank line ---
		{"body no blank line", "feat: add x\nbody", 1, "blank line"},

		// --- error: description lowercase ---
		{
			"uppercase description",
			fullMsg("feat: Add login endpoint", "- add route handler", "This adds the login route."),
			1, "lowercase",
		},

		// --- error: title too long ---
		{
			"title too long",
			fullMsg("feat: add a very long title that exceeds fifty characters here", "- add route", "This adds the route."),
			1, "50 characters",
		},

		// --- error: trailing period ---
		{
			"title ends with period",
			fullMsg("feat: add login endpoint.", "- add route handler", "This adds the login route."),
			1, "period",
		},

		// --- error: body required ---
		{"header only", "feat: add login endpoint", 1, "body is required"},

		// --- error: no bullet points ---
		{
			"body no bullets",
			"feat: add login endpoint\n\nJust some prose.\n\nMore prose.",
			1, "bullet point",
		},

		// --- error: body line too long ---
		{
			"body line too long",
			"feat: add login\n\n- add a route handler for the new login endpoint that is being introduced here\n\nThis adds the route.",
			1, "72 characters",
		},

		// --- error: no explanation after bullets ---
		{
			"no explanation after bullets",
			"feat: add login endpoint\n\n- add route handler\n- add session support",
			1, "explanation paragraph",
		},

		// --- Co-Authored-By values are free-form when non-empty ---
		{
			"co-authored-by with bare email",
			"feat: add login endpoint\n\n- add route handler\n\nThis adds the route.\n\nCo-Authored-By: Bot bot@example.com",
			0, "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sh", scriptPath)
			cmd.Stdin = strings.NewReader(makeInput(tc.msg))
			out, err := cmd.CombinedOutput()

			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("exec error: %v", err)
				}
			}

			if exitCode != tc.wantExit {
				t.Errorf("exit code = %d, want %d\noutput: %s", exitCode, tc.wantExit, out)
			}

			if tc.wantErr != "" && !strings.Contains(string(out), tc.wantErr) {
				t.Errorf("stderr missing %q\noutput: %s", tc.wantErr, out)
			}
		})
	}

	t.Run("non-English natural capitalization", func(t *testing.T) {
		msg := "feat: 修复登录问题\n\n- 添加登录端点\n\n这个修复处理了登录失败。"
		cmd := exec.Command("sh", scriptPath)
		cmd.Stdin = strings.NewReader(makeInputWithLanguage(msg, "auto", "修复登录问题"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("non-English hook failed: %v, output: %s", err, out)
		}
	})

	t.Run("explicit English retains lowercase", func(t *testing.T) {
		msg := fullMsg("feat: Add login endpoint", "- add route handler", "This adds the route.")
		cmd := exec.Command("sh", scriptPath)
		cmd.Stdin = strings.NewReader(makeInputWithLanguage(msg, "en-US", ""))
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "lowercase") {
			t.Fatalf("expected English lowercase failure, err=%v output=%s", err, out)
		}
	})

	t.Run("non-English title counts runes", func(t *testing.T) {
		msg := fullMsg("feat: ログイン機能を追加して認証処理を改善します", "- ログイン処理を更新", "認証の動作を改善します。")
		cmd := exec.Command("sh", scriptPath)
		cmd.Stdin = strings.NewReader(makeInputWithLanguage(msg, "Japanese", ""))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("non-English title length should count runes, err=%v output=%s", err, out)
		}
	})
}
