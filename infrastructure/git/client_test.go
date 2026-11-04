package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// gitOutput invokes git inside dir and returns its stdout, failing the test on
// error. Used to inspect the index after staging.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// chdirForTest moves into dir for the duration of the test and restores the
// original working directory afterwards. The git Client reads the repository
// from the process cwd.
func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

// newUnbornNestedRepo creates an embedded git repository with no commit checked
// out. git reports such a directory as a trailing-slash "gitlink candidate"
// entry, which `git add -f` cannot stage.
func newUnbornNestedRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	runGit(t, dir, "init", "-q")
}

// TestClient_CommitLog_CapsPerCommitFiles asserts that a single mega-commit
// (e.g. a vendor import touching hundreds of thousands of paths) contributes
// at most 300 file lines to its CommitLog entry, so the scope-generation
// prompt stays bounded regardless of history shape.
func TestClient_CommitLog_CapsPerCommitFiles(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	for i := 0; i < 350; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "feat: mega import")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	entries, err := NewClient().CommitLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("CommitLog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	// Entry format: "subject\n  file1\n  file2..." — count the file lines.
	fileLines := strings.Count(entries[0], "\n  ")
	if fileLines != 300 {
		t.Errorf("file lines in entry = %d, want 300 (capped)", fileLines)
	}
	if !strings.Contains(entries[0], "feat: mega import") {
		t.Errorf("entry missing subject: %q", entries[0])
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

func TestClient_TopLevelDirsFromSubdirUsesRepositoryRoot(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "application", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir application: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "infrastructure"), 0o755); err != nil {
		t.Fatalf("mkdir infrastructure: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git-agent"), 0o755); err != nil {
		t.Fatalf("mkdir .git-agent: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(filepath.Join(dir, "application", "nested")); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	dirs, err := NewClient().TopLevelDirs(context.Background())
	if err != nil {
		t.Fatalf("TopLevelDirs: %v", err)
	}
	if got, want := strings.Join(dirs, ","), "application,infrastructure"; got != want {
		t.Fatalf("TopLevelDirs() = %q, want %q", got, want)
	}
}

func TestClient_ProjectFilesFromSubdirAreRepositoryRelative(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "application"), 0o755); err != nil {
		t.Fatalf("mkdir application: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "application", "service.go"), []byte("package application\n"), 0o644); err != nil {
		t.Fatalf("write service.go: %v", err)
	}
	runGit(t, dir, "add", ".")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(filepath.Join(dir, "application")); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	files, err := NewClient().ProjectFiles(context.Background())
	if err != nil {
		t.Fatalf("ProjectFiles: %v", err)
	}
	if len(files) != 1 || files[0] != "application/service.go" {
		t.Fatalf("ProjectFiles() = %v, want [application/service.go]", files)
	}
}

// An embedded (nested) git repository is reported by `git ls-files --others`
// as a directory entry with a trailing slash — a gitlink candidate, not a file.
// git-agent must not treat it as a file to stage: `git add -f` on an unborn
// nested repo fails with "does not have a commit checked out" and aborts the
// whole commit, and on a committed one silently records a broken gitlink.
func TestClient_AllChangedFiles_OmitsEmbeddedRepoDirEntries(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	newUnbornNestedRepo(t, filepath.Join(dir, "work", "smoke"))
	// A regular untracked file beside it must still be reported and stageable.
	if err := os.WriteFile(filepath.Join(dir, "work", "keep.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write keep.txt: %v", err)
	}
	// A non-ASCII nested repo dir: git C-quotes the entry, so the trailing slash
	// only shows up after unquoting.
	newUnbornNestedRepo(t, filepath.Join(dir, "中文目录"))

	chdirForTest(t, dir)
	c := NewClient()
	ctx := context.Background()

	files, err := c.AllChangedFiles(ctx)
	if err != nil {
		t.Fatalf("AllChangedFiles: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "/") {
			t.Errorf("AllChangedFiles returned a directory entry %q; want only files", f)
		}
	}
	if len(files) != 1 || files[0] != "work/keep.txt" {
		t.Fatalf("AllChangedFiles() = %v, want [work/keep.txt]", files)
	}

	// The returned list must be stageable without aborting on the nested repos.
	if err := c.StageFiles(ctx, files); err != nil {
		t.Fatalf("StageFiles(%v): %v", files, err)
	}
	index := gitOutput(t, dir, "ls-files", "-s")
	if !strings.Contains(index, "work/keep.txt") {
		t.Errorf("keep.txt not staged; index:\n%s", index)
	}
	if strings.Contains(index, "160000") {
		t.Errorf("a nested repo was staged as a gitlink; index:\n%s", index)
	}
}

// StageFiles must skip trailing-slash directory entries even when a caller
// hands them over directly (e.g. a stale plan from an older run), instead of
// failing the whole group with git's "does not have a commit checked out".
func TestClient_StageFiles_SkipsDirEntries(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	newUnbornNestedRepo(t, filepath.Join(dir, "work", "smoke"))
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write keep.txt: %v", err)
	}

	chdirForTest(t, dir)
	c := NewClient()
	ctx := context.Background()

	if err := c.StageFiles(ctx, []string{"work/smoke/", "keep.txt"}); err != nil {
		t.Fatalf("StageFiles: %v", err)
	}
	index := gitOutput(t, dir, "ls-files", "-s")
	if !strings.Contains(index, "keep.txt") {
		t.Errorf("keep.txt not staged; index:\n%s", index)
	}
	if strings.Contains(index, "160000") || strings.Contains(index, "work/smoke") {
		t.Errorf("nested repo was staged; index:\n%s", index)
	}
}

// A committed embedded repo would otherwise be staged silently as a broken
// gitlink (160000). The StageFiles guard must skip it too, not just unborn
// repos that make git fail outright.
func TestClient_StageFiles_SkipsCommittedEmbeddedRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")

	nested := filepath.Join(dir, "work", "smoke")
	newUnbornNestedRepo(t, nested)
	if err := os.WriteFile(filepath.Join(nested, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	runGit(t, nested, "add", "f.txt")
	runGit(t, nested, "commit", "-q", "-m", "inner")
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write keep.txt: %v", err)
	}

	chdirForTest(t, dir)
	c := NewClient()

	// Feed the exact dir entry git would report, plus a real file.
	if err := c.StageFiles(context.Background(), []string{"work/smoke/", "keep.txt"}); err != nil {
		t.Fatalf("StageFiles: %v", err)
	}
	index := gitOutput(t, dir, "ls-files", "-s")
	if !strings.Contains(index, "keep.txt") {
		t.Errorf("keep.txt not staged; index:\n%s", index)
	}
	if strings.Contains(index, "160000") {
		t.Errorf("embedded repo staged as a gitlink; index:\n%s", index)
	}
}

// hashUntracked powers rename detection. A nested-repo dir entry makes
// `git hash-object --stdin-paths` fail for the whole batch, silently disabling
// worktree move detection. It must hash the real files and ignore dir entries.
func TestClient_DetectRenames_WorktreeMoveAlongsideEmbeddedRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "old.md"), []byte("moved verbatim\n"), 0o644); err != nil {
		t.Fatalf("write old.md: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")

	// Worktree move: old path deleted, new path untracked.
	if err := os.MkdirAll(filepath.Join(dir, "b"), 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "old.md"), filepath.Join(dir, "b", "new.md")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// An unborn nested repo alongside would poison hash-object --stdin-paths.
	newUnbornNestedRepo(t, filepath.Join(dir, "b", "embedded"))

	chdirForTest(t, dir)
	renames, err := NewClient().DetectRenames(context.Background())
	if err != nil {
		t.Fatalf("DetectRenames: %v", err)
	}
	if len(renames) != 1 || renames[0].Old != "old.md" || renames[0].New != "b/new.md" {
		t.Fatalf("expected [{old.md b/new.md}], got %+v", renames)
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

// A staged rename is reported by git as a single R line; --name-only would
// show only the new path, dropping the old-path deletion. AllChangedFiles must
// return BOTH so the deletion is planned and committed with the addition rather
// than left behind for a later commit.
func TestClient_AllChangedFiles_StagedRenameCapturesBothPaths(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "old.md"), []byte("keep this content\n"), 0o644); err != nil {
		t.Fatalf("write old.md: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")
	// Stage the move as a rename (git detects R because content is identical).
	runGit(t, dir, "mv", "old.md", "new.md")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	files, err := NewClient().AllChangedFiles(context.Background())
	if err != nil {
		t.Fatalf("AllChangedFiles: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		seen[f] = true
	}
	if !seen["old.md"] || !seen["new.md"] {
		t.Errorf("expected both old.md and new.md, got %v", files)
	}
}

func TestClient_DetectRenames_StagedMove(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "old.md"), []byte("some stable content\n"), 0o644); err != nil {
		t.Fatalf("write old.md: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")
	runGit(t, dir, "mv", "old.md", "new.md")

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	renames, err := NewClient().DetectRenames(context.Background())
	if err != nil {
		t.Fatalf("DetectRenames: %v", err)
	}
	if len(renames) != 1 || renames[0].Old != "old.md" || renames[0].New != "new.md" {
		t.Fatalf("expected [{old.md new.md}], got %+v", renames)
	}
}

// A worktree move leaves the new file untracked, so git's own rename detection
// cannot see it; DetectRenames must pair it by content hash instead.
func TestClient_DetectRenames_WorktreeMove(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "doc.md"), []byte("moved verbatim\n"), 0o644); err != nil {
		t.Fatalf("write a/doc.md: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")

	// Move on disk without staging: old path is a deletion, new path untracked.
	if err := os.MkdirAll(filepath.Join(dir, "b"), 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "a", "doc.md"), filepath.Join(dir, "b", "doc.md")); err != nil {
		t.Fatalf("rename: %v", err)
	}

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	renames, err := NewClient().DetectRenames(context.Background())
	if err != nil {
		t.Fatalf("DetectRenames: %v", err)
	}
	if len(renames) != 1 || renames[0].Old != "a/doc.md" || renames[0].New != "b/doc.md" {
		t.Fatalf("expected [{a/doc.md b/doc.md}], got %+v", renames)
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

// `ls-files --others` reports embedded git repositories as trailing-slash
// directory entries. splitUnquoted must drop them (after C-unquoting) while
// keeping ordinary file paths, including quoted non-ASCII names.
func TestSplitUnquoted_DropsDirEntries(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "plain dir entry dropped, file kept",
			input: "work/smoke/\nwork/keep.txt\n",
			want:  []string{"work/keep.txt"},
		},
		{
			name:  "C-quoted non-ASCII dir entry dropped",
			input: "\"\\344\\270\\255\\346\\226\\207/\"\nmain.go\n",
			want:  []string{"main.go"},
		},
		{
			name:  "only dir entries returns nil",
			input: "a/\nb/\n",
			want:  nil,
		},
		{
			name:  "ordinary quoted file kept and unquoted",
			input: "\"path with spaces.go\"\n",
			want:  []string{"path with spaces.go"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitUnquoted([]byte(tt.input))
			if len(got) != len(tt.want) {
				t.Fatalf("splitUnquoted() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitUnquoted()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
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
