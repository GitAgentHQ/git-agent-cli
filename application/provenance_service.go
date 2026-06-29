package application

import (
	"context"
	"fmt"
	"sort"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ProvenanceRow is one chronological change to a file in the Provenance View.
type ProvenanceRow struct {
	Seq          int64  `json:"seq"`
	When         int64  `json:"when"` // recorded_at, unix seconds
	Who          string `json:"who"`  // EventSource: claude-code | cursor | human | unknown
	Tool         string `json:"tool"`
	BeforeBlob   string `json:"before_blob"`   // File Blob Ref
	AfterBlob    string `json:"after_blob"`    // File Blob Ref
	ChangeKind   string `json:"change_kind"`   // A|M|D|R
	LinkedCommit string `json:"linked_commit"` // from action_produces, empty if none
	OutOfBand    bool   `json:"out_of_band"`   // true when Who == "unknown"
}

// ProvenanceView is the chronological, rename-aware history for one file.
type ProvenanceView struct {
	File string          `json:"file"`
	Rows []ProvenanceRow `json:"rows"`
}

// ProvenanceService builds a rename-aware, chronological change history for a
// file from the Event Log (event_files) plus Out-of-Band Events. Observed and
// out-of-band Events live in the same append-only events table, so a single
// ordered read over event_files (for the rename-resolved path set) covers both.
type ProvenanceService struct {
	repo graph.GraphRepository
}

// NewProvenanceService creates a ProvenanceService over the given repository.
func NewProvenanceService(repo graph.GraphRepository) *ProvenanceService {
	return &ProvenanceService{repo: repo}
}

// Provenance returns the chronological Provenance View for filePath. The queried
// path inherits the history of its pre-rename identities (via ResolveRenames),
// the rows merge Event-log changes and Out-of-Band Events, and out-of-band rows
// (source "unknown") are flagged.
func (s *ProvenanceService) Provenance(ctx context.Context, filePath string) (*ProvenanceView, error) {
	// 1. Resolve the file's prior identities so new.go inherits old.go's history.
	priors, err := s.repo.ResolveRenames(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve renames for %s: %w", filePath, err)
	}
	paths := append([]string{filePath}, priors...)

	// 2. Read event_files joined to their events row for the whole path set; this
	// one read covers observed and out-of-band Events alike.
	changes, err := s.repo.FileChanges(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("file changes for %s: %w", filePath, err)
	}

	rows := make([]ProvenanceRow, 0, len(changes))
	for _, c := range changes {
		rows = append(rows, ProvenanceRow{
			Seq:          c.Seq,
			When:         c.RecordedAt,
			Who:          c.Source,
			Tool:         c.ToolName,
			BeforeBlob:   c.BeforeBlob,
			AfterBlob:    c.AfterBlob,
			ChangeKind:   c.ChangeKind,
			LinkedCommit: c.LinkedCommit,
			OutOfBand:    graph.EventSource(c.Source) == graph.EventSourceUnknown,
		})
	}

	// 4. Order rows by global seq ascending — the chronological merge.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })

	return &ProvenanceView{File: filePath, Rows: rows}, nil
}
