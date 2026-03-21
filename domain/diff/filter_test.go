package diff_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/diff"
	infradiff "github.com/gitagenthq/git-agent/infrastructure/diff"
)

func newFilter() diff.DiffFilter {
	return infradiff.NewPatternFilter()
}

func TestPatternFilter_excludesLockFiles(t *testing.T) {
	f := newFilter()
	input := &diff.StagedDiff{
		Files:   []string{"go.sum", "main.go"},
		Content: "diff --git a/go.sum b/go.sum\n+hash line\ndiff --git a/main.go b/main.go\n+func main() {}\n",
		Lines:   4,
	}
	got, err := f.Filter(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Files list keeps all entries so lock files can still be staged/committed.
	if len(got.Files) != 2 {
		t.Errorf("expected 2 files (lock kept for staging), got %v", got.Files)
	}
	// Lock file content must be absent from the diff sent to LLM.
	if strings.Contains(got.Content, "go.sum") {
		t.Error("lock file go.sum should be excluded from diff content")
	}
}

func TestPatternFilter_excludesBinaryFiles(t *testing.T) {
	f := newFilter()
	input := &diff.StagedDiff{
		Files:   []string{"logo.png", "app.ts"},
		Content: "diff --git a/logo.png b/logo.png\nBinary files differ\ndiff --git a/app.ts b/app.ts\n+export {}\n",
		Lines:   4,
	}
	got, err := f.Filter(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Files list keeps all entries so binaries can still be staged/committed.
	if len(got.Files) != 2 {
		t.Errorf("expected 2 files (binary kept for staging), got %v", got.Files)
	}
	// Binary file content must be absent from the diff sent to LLM.
	if strings.Contains(got.Content, "logo.png") {
		t.Error("binary file logo.png should be excluded from diff content")
	}
}

func TestPatternFilter_mixedFiles(t *testing.T) {
	f := newFilter()
	input := &diff.StagedDiff{
		Files:   []string{"yarn.lock", "main.go", "icon.ico"},
		Content: "diff --git a/yarn.lock b/yarn.lock\n+lock\ndiff --git a/main.go b/main.go\n+code\ndiff --git a/icon.ico b/icon.ico\nBinary\n",
		Lines:   6,
	}
	got, err := f.Filter(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All files kept for staging; only content is filtered.
	if len(got.Files) != 3 {
		t.Errorf("expected 3 files, got %v", got.Files)
	}
	if strings.Contains(got.Content, "yarn.lock") || strings.Contains(got.Content, "icon.ico") {
		t.Error("lock/binary files should be excluded from diff content")
	}
	if !strings.Contains(got.Content, "main.go") {
		t.Error("main.go should remain in diff content")
	}
}

func TestPatternFilter_allFiltered_returnsError(t *testing.T) {
	f := newFilter()
	input := &diff.StagedDiff{
		Files:   []string{"package-lock.json", "logo.png"},
		Content: "diff --git a/package-lock.json b/package-lock.json\n+lock\ndiff --git a/logo.png b/logo.png\nBinary\n",
		Lines:   4,
	}
	_, err := f.Filter(context.Background(), input)
	if err == nil {
		t.Fatal("expected error when all files are filtered, got nil")
	}
	const want = "no staged text changes after filtering"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
