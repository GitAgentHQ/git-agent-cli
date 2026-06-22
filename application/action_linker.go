package application

import (
	"context"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ActionLinker links uncommitted actions to a newly created commit.
// A nil linker means action-to-commit linking is unavailable (graceful degradation).
type ActionLinker interface {
	LinkActionsToCommit(ctx context.Context, commitHash string, files []string) error
}

// GraphActionLinker implements ActionLinker using the graph repository.
type GraphActionLinker struct {
	repo graph.GraphRepository
}

// NewGraphActionLinker creates a new GraphActionLinker.
func NewGraphActionLinker(repo graph.GraphRepository) *GraphActionLinker {
	return &GraphActionLinker{repo: repo}
}

// LinkActionsToCommit finds unlinked actions that modified any of the given
// files and creates action_produces edges to the commit.
func (l *GraphActionLinker) LinkActionsToCommit(ctx context.Context, commitHash string, files []string) error {
	if len(files) == 0 || commitHash == "" {
		return nil
	}

	// Look back 24h for unlinked actions matching these files
	since := time.Now().Add(-24 * time.Hour).Unix()
	actions, err := l.repo.UnlinkedActionsForFiles(ctx, files, since)
	if err != nil {
		return err
	}

	for _, a := range actions {
		actionFiles := make(map[string]bool, len(a.FilesChanged))
		for _, f := range a.FilesChanged {
			actionFiles[f] = true
		}
		for _, f := range files {
			if !actionFiles[f] {
				continue
			}
			if err := l.repo.CreateActionProduces(ctx, a.ID, commitHash, f); err != nil {
				return err
			}
		}
	}
	return nil
}
