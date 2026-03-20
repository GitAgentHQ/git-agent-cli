package hooks_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fradser/ga-cli/hooks"
)

func TestConventionalHook(t *testing.T) {
	// Write embedded script to a temp file
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "conventional.sh")
	if err := os.WriteFile(scriptPath, hooks.Conventional, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	type payload struct {
		CommitMessage string   `json:"commit_message"`
		Diff          string   `json:"diff"`
		Intent        string   `json:"intent"`
		StagedFiles   []string `json:"staged_files"`
		Config        struct {
			Scopes []string `json:"scopes"`
		} `json:"config"`
	}

	makeInput := func(msg string) string {
		p := payload{
			CommitMessage: msg,
			StagedFiles:   []string{},
		}
		b, _ := json.Marshal(p)
		return string(b)
	}

	cases := []struct {
		name     string
		msg      string
		wantExit int
		wantErr  string
	}{
		{"simple feat", "feat: add login", 0, ""},
		{"scoped fix", "fix(parser): handle null", 0, ""},
		{"breaking bang", "feat!: drop Node 12", 0, ""},
		{"breaking bang scoped", "feat(api)!: remove endpoint", 0, ""},
		{"with body blank line", "feat: add x\n\nbody here", 0, ""},
		{"body + BREAKING CHANGE footer", "fix: x\n\nbody\n\nBREAKING CHANGE: removed", 0, ""},
		{"BREAKING-CHANGE footer", "fix: x\n\nbody\n\nBREAKING-CHANGE: removed", 0, ""},
		{"Co-Authored-By footer", "feat: x\n\nCo-Authored-By: A <a@b>", 0, ""},
		{"escaped quotes", `feat: handle "quoted" strings`, 0, ""},
		{"missing type", "add login feature", 1, "Conventional Commits"},
		{"missing colon space", "feat add login", 1, ""},
		{"empty description", "feat:", 1, ""},
		{"body no blank line", "feat: add x\nbody", 1, "blank line"},
		{"invalid type", "feature: add login", 1, ""},
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
}
