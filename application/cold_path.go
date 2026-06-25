package application

import (
	"context"
	"fmt"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// SyncEventLog runs the cold Enrichment path: rebuild projections from the Event
// Log, reconcile unexplained working-tree changes into out-of-band Events, then
// rebuild again when any were appended so event_files stay derived solely from
// Replay.
func SyncEventLog(ctx context.Context, repo graph.GraphRepository, git graph.GraphGitClient) (ReconcileResult, error) {
	rb := NewProjectionRebuilder(repo, git)
	if err := rb.Rebuild(ctx); err != nil {
		return ReconcileResult{}, fmt.Errorf("rebuild projections: %w", err)
	}

	res, err := NewReconcileService(repo, git).Reconcile(ctx)
	if err != nil {
		return res, fmt.Errorf("reconcile: %w", err)
	}

	if res.OutOfBandAppended > 0 {
		if err := rb.Rebuild(ctx); err != nil {
			return res, fmt.Errorf("rebuild after reconcile: %w", err)
		}
	}

	return res, nil
}
