package diff_test

import (
	"context"
	"strings"
	"testing"
	"time"
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
	// Hard cut at the byte budget — no seeking back to an early newline, so the
	// bulk of the diff survives instead of being discarded to the header.
	if len(out.Content) != 2500 {
		t.Fatalf("expected content kept up to the 2500-byte cap, got %d bytes", len(out.Content))
	}
	if out.Content != content[:2500] {
		t.Fatalf("expected the first 2500 bytes verbatim")
	}
}

func TestTruncate_LineAndByteCapTogether(t *testing.T) {
	tr := infraDiff.NewLineTruncator()
	// 10 lines of 100 chars each: the line cap keeps 5, the byte cap then
	// shaves the result down further.
	line := strings.Repeat("y", 100)
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = line
	}
	in := &domainDiff.StagedDiff{Files: []string{"a.go"}, Content: strings.Join(lines, "\n"), Lines: 10}

	out, did, err := tr.Truncate(context.Background(), in, 5, 300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !did {
		t.Fatalf("expected truncation")
	}
	// Line cap -> 5 lines (~504 bytes), byte cap -> at most 300 bytes.
	if len(out.Content) != 300 {
		t.Fatalf("expected 300 bytes after line+byte caps, got %d", len(out.Content))
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

func TestTruncate_MidStringInvalidUTF8_NoHang(t *testing.T) {
	tr := infraDiff.NewLineTruncator()
	// One newline-free line over the cap with an invalid UTF-8 byte at the
	// midpoint. The previous whole-string validation looped O(n^2) here (minutes
	// of CPU); inspecting only the trailing rune must return promptly and leave
	// the mid-string byte in place.
	const maxBytes = 1 << 20 // 1 MiB
	b := make([]byte, maxBytes*2)
	for i := range b {
		b[i] = 'x'
	}
	b[maxBytes/2] = 0xFF // invalid lead byte, mid-string (worst case for the old loop)
	in := &domainDiff.StagedDiff{Files: []string{"big.min.js"}, Content: string(b), Lines: 1}

	done := make(chan struct{})
	var out *domainDiff.StagedDiff
	go func() {
		out, _, _ = tr.Truncate(context.Background(), in, 0, maxBytes)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Truncate did not return within 5s — O(n^2) UTF-8 regression")
	}
	// Trailing bytes are ASCII, so the cut keeps the full budget; the invalid
	// mid-string byte is preserved (the JSON encoder handles it downstream).
	if len(out.Content) != maxBytes {
		t.Fatalf("expected %d bytes kept, got %d", maxBytes, len(out.Content))
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
