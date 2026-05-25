package diff_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	domainDiff "github.com/gitagenthq/git-agent/domain/diff"
	infraDiff "github.com/gitagenthq/git-agent/infrastructure/diff"
)

func TestTruncate_WithinBothLimits_Unchanged(t *testing.T) {
	tr := infraDiff.NewLineTruncator()
	in := &domainDiff.StagedDiff{Files: []string{"a.go"}, Content: "+a\n+b\n+c", Lines: 3}

	out, did, err := tr.Truncate(context.Background(), in, 10, 1<<20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did {
		t.Fatalf("did not expect truncation")
	}
	if out.Content != in.Content {
		t.Fatalf("content changed: %q", out.Content)
	}
}

func TestTruncate_LineCap(t *testing.T) {
	tr := infraDiff.NewLineTruncator()
	in := &domainDiff.StagedDiff{
		Files:   []string{"a.go"},
		Content: "l1\nl2\nl3\nl4\nl5",
		Lines:   5,
	}

	out, did, err := tr.Truncate(context.Background(), in, 2, 1<<20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !did {
		t.Fatalf("expected truncation")
	}
	if out.Content != "l1\nl2" {
		t.Fatalf("got %q, want %q", out.Content, "l1\nl2")
	}
}

func TestTruncate_ByteCap_LongLinesUnderLineCap(t *testing.T) {
	tr := infraDiff.NewLineTruncator()
	// 5 lines (well under the line cap) but each line is huge, so the byte
	// cap must kick in where the line cap cannot.
	long := strings.Repeat("x", 1000)
	content := strings.Join([]string{long, long, long, long, long}, "\n")
	in := &domainDiff.StagedDiff{Files: []string{"vendor.min.js"}, Content: content, Lines: 5}

	out, did, err := tr.Truncate(context.Background(), in, 100, 2500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !did {
		t.Fatalf("expected truncation")
	}
	if len(out.Content) > 2500 {
		t.Fatalf("content %d bytes exceeds cap 2500", len(out.Content))
	}
	// Cut on a line boundary: two full 1000-byte lines + one separator = 2001.
	if out.Content != long+"\n"+long {
		t.Fatalf("expected cut on a line boundary, got %d bytes", len(out.Content))
	}
}

func TestTruncate_SingleOversizedLine_ValidUTF8(t *testing.T) {
	tr := infraDiff.NewLineTruncator()
	// One line, no newline, built from a multi-byte rune so a naive byte cut
	// would split a rune.
	content := strings.Repeat("世", 1000) // 3 bytes each => 3000 bytes
	in := &domainDiff.StagedDiff{Files: []string{"big.txt"}, Content: content, Lines: 1}

	out, did, err := tr.Truncate(context.Background(), in, 0, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !did {
		t.Fatalf("expected truncation")
	}
	if len(out.Content) > 2000 {
		t.Fatalf("content %d bytes exceeds cap 2000", len(out.Content))
	}
	if !utf8.ValidString(out.Content) {
		t.Fatalf("content is not valid UTF-8")
	}
}

func TestTruncate_DisabledCaps_NoOp(t *testing.T) {
	tr := infraDiff.NewLineTruncator()
	in := &domainDiff.StagedDiff{Files: []string{"a.go"}, Content: "l1\nl2\nl3", Lines: 3}

	out, did, err := tr.Truncate(context.Background(), in, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did {
		t.Fatalf("did not expect truncation")
	}
	if out.Content != in.Content {
		t.Fatalf("content changed: %q", out.Content)
	}
}
