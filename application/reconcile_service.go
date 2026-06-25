package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ReconcileService is the cold-path Blind-Spot net. It reintroduces diff-based
// detection as a fallback only: any working-tree file whose content diverges from
// the state the Event Log accounts for — and which no recent Event explains — is
// recorded as a synthetic out-of-band Event chained into the same Event Log, so
// every byte that reaches a commit is attributable to an observed or an explicit
// out-of-band Event.
type ReconcileService struct {
	repo graph.GraphRepository
	git  graph.GraphGitClient
}

// NewReconcileService creates a ReconcileService reusing GraphGitClient's
// DiffNameOnly/HashObject — the exact calls removed from the capture hot path.
func NewReconcileService(repo graph.GraphRepository, git graph.GraphGitClient) *ReconcileService {
	return &ReconcileService{repo: repo, git: git}
}

// ReconcileResult reports what the pass appended.
type ReconcileResult struct {
	OutOfBandAppended int
	ResidualFiles     []string
}

// Reconcile detects the unexplained residual and appends one out-of-band Event
// per residual file. Candidates come from the working-tree diff; a file is
// residual only when its current blob diverges from the last after_blob the log
// recorded and no Event already accounts for that content.
func (s *ReconcileService) Reconcile(ctx context.Context) (ReconcileResult, error) {
	changed, err := s.git.DiffNameOnly(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("diff name-only: %w", err)
	}

	// Sort candidates so the appended Events get a deterministic seq order.
	candidates := make([]string, 0, len(changed))
	for _, f := range changed {
		if graph.IsToolingPath(f) {
			continue
		}
		candidates = append(candidates, f)
	}
	sort.Strings(candidates)

	var res ReconcileResult
	for _, file := range candidates {
		current, err := s.git.HashObject(ctx, file)
		if err != nil {
			return res, fmt.Errorf("hash object %s: %w", file, err)
		}

		lastKnown, known, err := s.repo.LatestAfterBlob(ctx, file)
		if err != nil {
			return res, err
		}
		// Explained: the log already accounts for the current content (a recent
		// captured Edit/Write whose after_blob matches the working tree).
		if known && lastKnown == current {
			continue
		}

		e := graph.EventRecord{
			EventID:    outOfBandEventID(file, lastKnown, current),
			RecordedAt: time.Now().Unix(),
			Source:     graph.EventSourceUnknown,
			InstanceID: "",
			Kind:       graph.EventKindOutOfBand,
			ToolName:   "external-edit",
			PayloadRaw: marshalOutOfBandPayload(file, lastKnown, current),
		}
		if _, err := s.repo.AppendEvent(ctx, e); err != nil {
			return res, fmt.Errorf("append out-of-band event for %s: %w", file, err)
		}

		res.OutOfBandAppended++
		res.ResidualFiles = append(res.ResidualFiles, file)
	}

	return res, nil
}

// outOfBandPayload is the JSON unit stored on a synthetic out-of-band Event: a
// compact statement of the file and its before/after File Blob Refs. It is the
// single shared contract between reconcile (the producer, marshalOutOfBandPayload)
// and projection Replay (the consumer, extractOutOfBandFileProjections), so the
// wire shape lives in one place rather than a Sprintf template and a struct that
// must be kept in lockstep. It carries no user content (only OIDs).
type outOfBandPayload struct {
	OutOfBand outOfBandFile `json:"out_of_band"`
}

type outOfBandFile struct {
	FilePath   string `json:"file_path"`
	BeforeBlob string `json:"before_blob"`
	AfterBlob  string `json:"after_blob"`
}

// marshalOutOfBandPayload renders the hashed unit via encoding/json so any path
// (backslashes, control bytes, non-UTF-8) is encoded as valid JSON the consumer
// can parse, rather than a hand-rolled %q template.
func marshalOutOfBandPayload(file, beforeBlob, afterBlob string) []byte {
	b, _ := json.Marshal(outOfBandPayload{OutOfBand: outOfBandFile{
		FilePath:   file,
		BeforeBlob: beforeBlob,
		AfterBlob:  afterBlob,
	}})
	return b
}

// outOfBandEventID derives a stable id from the residual so re-running the pass
// over the same unexplained change yields the same id; the events.event_id UNIQUE
// constraint then dedupes a repeated reconcile rather than forging a new Event.
func outOfBandEventID(file, beforeBlob, afterBlob string) string {
	sum := sha256.Sum256([]byte("out-of-band\x00" + file + "\x00" + beforeBlob + "\x00" + afterBlob))
	return "oob-" + hex.EncodeToString(sum[:12])
}
