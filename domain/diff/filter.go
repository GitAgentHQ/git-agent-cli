package diff

import "context"

// DiffFilter filters a staged diff to remove noise.
type DiffFilter interface {
	Filter(ctx context.Context, diff *StagedDiff) (*StagedDiff, error)
}
