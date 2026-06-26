package application

import (
	"context"
	"fmt"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// SyncSummary is the result of an incremental sync: the Event Log high-water
// mark, the projection high-water mark before sync, whether they already
// matched (no-op), whether a replay actually ran, and how many out-of-band
// Events a reconcile appended.
type SyncSummary struct {
	MaxEventSeq       int64 `json:"max_event_seq"`
	MaxProjectedSeq   int64 `json:"max_projected_seq"` // pre-sync projection high-water mark
	UpToDate          bool  `json:"up_to_date"`        // projections were already current (no-op)
	Replayed          bool  `json:"replayed"`          // an incremental replay ran
	OutOfBandAppended int   `json:"out_of_band_appended"`
}

// SyncIfStale brings the derived projections up to date with the Event Log. It
// is a no-op when the projections already reflect the latest event seq;
// otherwise it INCREMENTALLY replays only the new events (no reset) and
// reconciles unexplained working-tree changes into out-of-band Events, folding
// those too when any were appended. The staleness check is two indexed
// aggregates, so the common up-to-date case avoids VerifyChain + any replay.
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
	summary.Replayed = true

	rb := NewProjectionRebuilder(repo, git)
	if err := rb.SyncIncremental(ctx, maxProjected); err != nil {
		return summary, fmt.Errorf("incremental replay: %w", err)
	}

	res, err := NewReconcileService(repo, git).Reconcile(ctx)
	if err != nil {
		return summary, fmt.Errorf("reconcile: %w", err)
	}
	if res.OutOfBandAppended > 0 {
		// Out-of-band Events were appended at seq > maxProjected; fold them.
		if err := rb.SyncIncremental(ctx, maxProjected); err != nil {
			return summary, fmt.Errorf("incremental replay after reconcile: %w", err)
		}
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
