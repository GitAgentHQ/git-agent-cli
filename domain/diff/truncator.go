package diff

import "context"

// DiffTruncator truncates a staged diff to fit within token limits.
type DiffTruncator interface {
	Truncate(ctx context.Context, diff *StagedDiff, maxLines int) (*StagedDiff, bool, error)
}
