package application

import (
	"context"
	"fmt"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// SyncSummary is the result of an incremental sync: the Event Log high-water
// mark, the projection high-water mark, whether they match, and how many
// out-of-band Events a reconcile appended when a replay ran.
type SyncSummary struct {
	MaxEventSeq       int64 `json:"max_event_seq"`
	MaxProjectedSeq   int64 `json:"max_projected_seq"`
	UpToDate          bool  `json:"up_to_date"`
	OutOfBandAppended int   `json:"out_of_band_appended"`
}

// SyncIfStale brings the derived projections up to date with the Event Log. It
// is a no-op when the projections already reflect the latest event seq;
// otherwise it runs the full cold path (SyncEventLog). The staleness check is
// two indexed aggregates, so the common up-to-date case avoids VerifyChain +
// ResetProjections + a full event replay.
func SyncIfStale(ctx context.Context, repo graph.GraphRepository, git graph.GraphGitClient) (SyncSummary, error) {
	maxEvent, err := repo.MaxEventSeq(ctx)
	if err != nil {
		return SyncSummary{}, fmt.Errorf("max event seq: %w", err)
	}
	maxProjected, err := repo.MaxProjectedEventSeq(ctx)
	if err != nil {
		return SyncSummary{}, fmt.Errorf("max projected seq: %w", err)
	}
	summary := SyncSummary{MaxEventSeq: maxEvent, MaxProjectedSeq: maxProjected, UpToDate: maxProjected >= maxEvent}
	if summary.UpToDate {
		return summary, nil
	}
	res, err := SyncEventLog(ctx, repo, git)
	if err != nil {
		return summary, err
	}
	summary.OutOfBandAppended = res.OutOfBandAppended
	return summary, nil
}

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
