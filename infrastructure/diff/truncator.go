package diff

import (
	"context"
	"strings"
	"unicode/utf8"

	domainDiff "github.com/gitagenthq/git-agent/domain/diff"
)

type lineTruncator struct{}

func NewLineTruncator() domainDiff.DiffTruncator {
	return &lineTruncator{}
}

func (t *lineTruncator) Truncate(_ context.Context, d *domainDiff.StagedDiff, maxLines, maxBytes int) (*domainDiff.StagedDiff, bool, error) {
	content := d.Content
	truncated := false

	// Line cap first — the soft, user-tunable limit.
	if maxLines > 0 && d.Lines > maxLines {
		lines := strings.SplitN(content, "\n", maxLines+1)
		content = strings.Join(lines[:maxLines], "\n")
		truncated = true
	}

	// Byte cap second — the hard guard. Long lines (vendored or minified
	// files) blow past the request-body limit while staying under maxLines,
	// so the line cap alone cannot prevent an oversized request.
	if maxBytes > 0 && len(content) > maxBytes {
		content = truncateBytes(content, maxBytes)
		truncated = true
	}

	if !truncated {
		return d, false, nil
	}

	return &domainDiff.StagedDiff{
		Files:   d.Files,
		Content: content,
		Lines:   strings.Count(content, "\n"),
	}, true, nil
}

// truncateBytes returns the largest prefix of s no longer than maxBytes,
// preferring a line boundary so no partial line is emitted. When a single line
// already exceeds maxBytes it backs off to a valid UTF-8 boundary instead.
func truncateBytes(s string, maxBytes int) string {
	cut := s[:maxBytes]
	if idx := strings.LastIndexByte(cut, '\n'); idx >= 0 {
		return cut[:idx]
	}
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
