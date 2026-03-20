package diff

import (
	"context"
	"errors"
	"strings"

	domainDiff "github.com/fradser/git-agent/domain/diff"
	"github.com/fradser/git-agent/pkg/filter"
)

type patternFilter struct{}

func NewPatternFilter() domainDiff.DiffFilter {
	return &patternFilter{}
}

func (f *patternFilter) Filter(_ context.Context, d *domainDiff.StagedDiff) (*domainDiff.StagedDiff, error) {
	var kept []string
	for _, file := range d.Files {
		if !filter.IsFiltered(file) {
			kept = append(kept, file)
		}
	}
	if len(kept) == 0 {
		return nil, errors.New("no staged text changes after filtering")
	}

	content := filterContent(d.Content, kept)
	lines := strings.Count(content, "\n")

	return &domainDiff.StagedDiff{
		Files:   kept,
		Content: content,
		Lines:   lines,
	}, nil
}

func filterContent(content string, kept []string) string {
	keptSet := make(map[string]bool, len(kept))
	for _, f := range kept {
		keptSet[f] = true
	}

	const prefix = "diff --git "
	parts := strings.Split(content, prefix)

	var sb strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		// first line is "a/<file> b/<file>", extract the b-side filename
		firstLine := part
		if idx := strings.IndexByte(part, '\n'); idx >= 0 {
			firstLine = part[:idx]
		}
		// "a/foo/bar.go b/foo/bar.go" -> take after last " b/"
		bIdx := strings.LastIndex(firstLine, " b/")
		if bIdx < 0 {
			continue
		}
		filename := firstLine[bIdx+3:]
		if keptSet[filename] {
			sb.WriteString(prefix)
			sb.WriteString(part)
		}
	}
	return sb.String()
}
