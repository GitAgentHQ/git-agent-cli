package diff

import "context"

// DiffTruncator truncates a staged diff so the request body stays within
// provider limits. maxLines bounds the line count (a soft, user-tunable cap);
// maxBytes bounds the byte size (a hard guard against the API request-body
// limit). A value <= 0 disables the corresponding cap. The bool reports
// whether either cap altered the diff.
type DiffTruncator interface {
	Truncate(ctx context.Context, diff *StagedDiff, maxLines, maxBytes int) (*StagedDiff, bool, error)
}
