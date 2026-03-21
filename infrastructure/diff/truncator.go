package diff

import (
	"context"
	"strings"

	domainDiff "github.com/gitagenthq/git-agent/domain/diff"
)

type lineTruncator struct{}

func NewLineTruncator() domainDiff.DiffTruncator {
	return &lineTruncator{}
}

func (t *lineTruncator) Truncate(_ context.Context, d *domainDiff.StagedDiff, maxLines int) (*domainDiff.StagedDiff, bool, error) {
	if maxLines <= 0 || d.Lines <= maxLines {
		return d, false, nil
	}

	lines := strings.SplitN(d.Content, "\n", maxLines+1)
	content := strings.Join(lines[:maxLines], "\n")

	return &domainDiff.StagedDiff{
		Files:   d.Files,
		Content: content,
		Lines:   maxLines,
	}, true, nil
}
