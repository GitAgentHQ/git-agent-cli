package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pkgerrors "github.com/gitagenthq/git-agent/pkg/errors"
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

func TestClient_AllChangedFiles_NonASCIIPath(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	const name = "项目复盘.md"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("content\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	c := NewClient()
	files, err := c.AllChangedFiles(context.Background())
	if err != nil {
		t.Fatalf("AllChangedFiles: %v", err)
	}

	var found bool
	for _, f := range files {
		if f == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected verbatim path %q in %v", name, files)
	}

	// The verbatim path must round-trip back into a pathspec for staging.
	if err := c.StageFiles(context.Background(), files); err != nil {
		t.Fatalf("StageFiles(%v): %v", files, err)
	}
}

func TestClient_Commit_NothingToCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Nothing staged: git prints "nothing to commit" to stdout and exits 1.
	_, err = NewClient().Commit(context.Background(), "feat(cli): noop")
	if !errors.Is(err, pkgerrors.ErrNothingToCommit) {
		t.Fatalf("expected ErrNothingToCommit, got %v", err)
	}
}

// TestClient_UntrackFile verifies that UntrackFile removes a path from the git
// index while keeping the working-tree file, and that IsTracked reflects the
// change. This is the contract init relies on to stop tracking .git-agent/graph.db.
func TestClient_UntrackFile(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	dbPath := filepath.Join(dir, ".git-agent", "graph.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("sqlite-ish"), 0o644); err != nil {
		t.Fatalf("write graph.db: %v", err)
	}
	runGit(t, dir, "add", ".git-agent/graph.db")
	runGit(t, dir, "commit", "-q", "-m", "add graph.db")

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	c := NewClient()
	ctx := context.Background()

	tracked, err := c.IsTracked(ctx, ".git-agent/graph.db")
	if err != nil {
		t.Fatalf("IsTracked: %v", err)
	}
	if !tracked {
		t.Fatal("graph.db should be tracked after git add")
	}

	if err := c.UntrackFile(ctx, ".git-agent/graph.db"); err != nil {
		t.Fatalf("UntrackFile: %v", err)
	}

	tracked, err = c.IsTracked(ctx, ".git-agent/graph.db")
	if err != nil {
		t.Fatalf("IsTracked after untrack: %v", err)
	}
	if tracked {
		t.Error("graph.db should not be tracked after UntrackFile")
	}
	// Working-tree file must remain (UntrackFile uses --cached).
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("working-tree graph.db must still exist after untrack: %v", err)
	}
}

// TestClient_IsTracked_NotTracked confirms IsTracked returns false (not an error)
// for a path git does not know about.
func TestClient_IsTracked_NotTracked(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	c := NewClient()
	tracked, err := c.IsTracked(context.Background(), ".git-agent/graph.db")
	if err != nil {
		t.Fatalf("IsTracked on untracked path: %v", err)
	}
	if tracked {
		t.Error("IsTracked should return false for a never-tracked path")
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
