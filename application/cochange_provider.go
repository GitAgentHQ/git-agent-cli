package application

import (
	"context"

	"github.com/gitagenthq/git-agent/domain/commit"
)

// CoChangeProvider supplies co-change hints for commit planning.
// nil provider means co-change is unavailable (graceful degradation).
type CoChangeProvider interface {
	GetHintsForFiles(ctx context.Context, files []string) ([]commit.CoChangeHint, error)
}
