package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitUnquote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "passthrough unquoted",
			input: "main.go",
			want:  "main.go",
		},
		{
			name:  "strips quotes",
			input: `"main.go"`,
			want:  "main.go",
		},
		{
			name:  "unescape backslash",
			input: `"path\\file"`,
			want:  `path\file`,
		},
		{
			name:  "unescape double quote",
			input: `"say\"hi\""`,
			want:  `say"hi"`,
		},
		{
			name:  "unescape tab",
			input: `"col1\tcol2"`,
			want:  "col1\tcol2",
		},
		{
			name:  "unescape newline",
			input: `"line1\nline2"`,
			want:  "line1\nline2",
		},
		{
			name: "octal UTF-8 sequence",
			// \303\251 is the UTF-8 encoding of é
			input: `"\303\251"`,
			want:  "é",
		},
		{
			name:  "empty quoted string",
			input: `""`,
			want:  "",
		},
		{
			name:  "not quoted - only opening quote",
			input: `"hello`,
			want:  `"hello`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitUnquote(tt.input)
			if got != tt.want {
				t.Errorf("gitUnquote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// runGit invokes git with the given args inside dir. Test helper that fails the
// test on non-zero exit so setup failures surface immediately.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestClient_StagedDiffNumStat(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	// Generate a ~1 MB plain-text file (16 chars per line * ~65536 lines).
	const lineCount = 1 << 16
	const lineText = "0123456789abcdef\n"
	var buf bytes.Buffer
	buf.Grow(lineCount * len(lineText))
	for i := 0; i < lineCount; i++ {
		buf.WriteString(lineText)
	}
	target := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(target, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write big.txt: %v", err)
	}
	runGit(t, dir, "add", "big.txt")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	c := NewClient()
	got, err := c.StagedDiffNumStat(context.Background())
	if err != nil {
		t.Fatalf("StagedDiffNumStat: %v", err)
	}
	if !strings.Contains(got, "big.txt") {
		t.Errorf("expected output to contain filename 'big.txt', got: %q", got)
	}
	// git diff --numstat outputs <adds>\t<dels>\t<path>
	if !strings.Contains(got, "65536\t0\tbig.txt") {
		t.Errorf("expected output to contain '65536\\t0\\tbig.txt', got: %q", got)
	}
}

func TestClient_AllChangedFilesFromSubdirIsStageable(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	// Untracked file inside a subdirectory.
	sub := filepath.Join(dir, "skills")
	if err := os.MkdirAll(filepath.Join(sub, "substore-openclash"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "substore-openclash", "SKILL.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	// Invoke from the subdirectory — this is where the path mismatch surfaced.
	if err := os.Chdir(sub); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	c := NewClient()
	ctx := context.Background()
	files, err := c.AllChangedFiles(ctx)
	if err != nil {
		t.Fatalf("AllChangedFiles: %v", err)
	}

	const want = "skills/substore-openclash/SKILL.md"
	found := false
	for _, f := range files {
		if f == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected root-relative path %q in %v", want, files)
	}

	// The path must be stageable, since StageFiles adds from the repo root.
	if err := c.StageFiles(ctx, []string{want}); err != nil {
		t.Fatalf("StageFiles: %v", err)
	}
}

func TestParseNameStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "modified file",
			input: "M\tmain.go\n",
			want:  []string{"main.go"},
		},
		{
			name:  "added file",
			input: "A\tnew.go\n",
			want:  []string{"new.go"},
		},
		{
			name:  "deleted file",
			input: "D\told.go\n",
			want:  []string{"old.go"},
		},
		{
			name:  "rename emits both paths",
			input: "R100\told.go\tnew.go\n",
			want:  []string{"old.go", "new.go"},
		},
		{
			name:  "copy emits both paths",
			input: "C100\tsrc.go\tdst.go\n",
			want:  []string{"src.go", "dst.go"},
		},
		{
			name:  "mixed lines",
			input: "M\tmain.go\nR100\ta.go\tb.go\nD\tc.go\n",
			want:  []string{"main.go", "a.go", "b.go", "c.go"},
		},
		{
			name:  "quoted path",
			input: "M\t\"path with spaces.go\"\n",
			want:  []string{"path with spaces.go"},
		},
		{
			name:  "deduplication",
			input: "M\tmain.go\nM\tmain.go\n",
			want:  []string{"main.go"},
		},
		{
			name:  "empty input returns nil",
			input: "",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNameStatus([]byte(tt.input))
			if len(got) != len(tt.want) {
				t.Fatalf("parseNameStatus() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseNameStatus()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
