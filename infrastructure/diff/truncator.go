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
	actualLines := d.Lines
	if len(content) > 0 && content[len(content)-1] != '\n' {
		actualLines++
	}
	if maxLines > 0 && actualLines > maxLines {
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
// dropping only a trailing partial multi-byte rune so the cut lands on a valid
// UTF-8 boundary. It deliberately does not seek a line boundary: a single
// oversized line (a minified or vendored blob) would otherwise be discarded
// back to an early newline, starving the LLM of the actual change. Mid-string
// invalid bytes are left alone — the JSON encoder substitutes U+FFFD downstream.
//
// The trim loop is bounded by utf8.UTFMax because a partial rune is at most
// 3 bytes long; on pathological input (e.g. unbroken continuation bytes) the
// loop stops after that, leaving any remaining malformed bytes in place rather
// than O(n) trimming back to the first lead byte.
func truncateBytes(s string, maxBytes int) string {
	cut := s[:maxBytes]
	for i := 0; i < utf8.UTFMax && len(cut) > 0; i++ {
		if r, size := utf8.DecodeLastRuneInString(cut); r == utf8.RuneError && size <= 1 {
			cut = cut[:len(cut)-1]
			continue
		}
		break
	}
	return cut
}
