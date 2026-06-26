package application_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infragraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// newProvenanceTestRepo opens a real on-disk SQLiteRepository over a temp
// graph.db so the rename-aware merge runs against the actual schema, not a
// stand-in. Provenance is a pure read over the Event Log + Projections, so the
// real SQLite driver plus the in-repo ResolveRenames seam are the unit under
// test.
func newProvenanceTestRepo(t *testing.T) *infragraph.SQLiteRepository {
	t.Helper()
	ctx := context.Background()
	client := infragraph.NewSQLiteClient(filepath.Join(t.TempDir(), "graph.db"))
	repo := infragraph.NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return repo
}

// appendProvEvent appends one Event through the real append path (so prev/this
// hashes and seq are genuine) and records its single touched-file row with the
// given File Blob Refs. Returns the persisted Event.
func appendProvEvent(t *testing.T, repo *infragraph.SQLiteRepository, e graph.EventRecord, file, beforeBlob, afterBlob, changeKind string) graph.EventRecord {
	t.Helper()
	ctx := context.Background()
	got, err := repo.AppendEvent(ctx, e)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := repo.CreateEventFile(ctx, graph.EventFile{
		EventSeq:   got.Seq,
		FilePath:   file,
		BeforeBlob: beforeBlob,
		AfterBlob:  afterBlob,
		ChangeKind: changeKind,
	}); err != nil {
		t.Fatalf("CreateEventFile: %v", err)
	}
	return got
}

// seedRenameFixture reproduces the scenario: file "old.go" was edited by an
// observed Event (seq 1), renamed to "new.go" (seq 2), then changed out-of-band
// on "new.go" (seq 3). All three changes live on the single chain in seq order.
func seedRenameFixture(t *testing.T, repo *infragraph.SQLiteRepository) {
	t.Helper()
	ctx := context.Background()

	// seq 1 — observed Edit on old.go (claude-code).
	appendProvEvent(t, repo, graph.EventRecord{
		EventID:    "evt-edit-old",
		RecordedAt: 1000,
		Source:     graph.EventSourceClaudeCode,
		InstanceID: "agent-A",
		Kind:       graph.EventKindTool,
		ToolName:   "Edit",
		PayloadRaw: editPayload("old.go", "x", "y"),
	}, "old.go", "blob-old-0", "blob-old-1", "M")

	// seq 2 — the rename old.go -> new.go, recorded as an observed Event and as a
	// renames-table edge so ResolveRenames folds old.go's history into new.go.
	appendProvEvent(t, repo, graph.EventRecord{
		EventID:    "evt-rename",
		RecordedAt: 1010,
		Source:     graph.EventSourceClaudeCode,
		InstanceID: "agent-A",
		Kind:       graph.EventKindTool,
		ToolName:   "Bash",
		PayloadRaw: []byte(`{"tool_name":"Bash","tool_input":{"command":"git mv old.go new.go"}}`),
	}, "new.go", "blob-old-1", "blob-new-1", "R")
	if err := repo.CreateRename(ctx, "old.go", "new.go", "commit-rename"); err != nil {
		t.Fatalf("CreateRename: %v", err)
	}

	// seq 3 — an out-of-band change on new.go (source unknown).
	appendProvEvent(t, repo, graph.EventRecord{
		EventID:    "evt-oob",
		RecordedAt: 1020,
		Source:     graph.EventSourceUnknown,
		InstanceID: "",
		Kind:       graph.EventKindOutOfBand,
		ToolName:   "external-edit",
		PayloadRaw: []byte(`{"out_of_band":{"file_path":"new.go","before_blob":"blob-new-1","after_blob":"blob-new-2"}}`),
	}, "new.go", "blob-new-1", "blob-new-2", "M")
}

func TestProvenance_RenameAwareChronologicalMerge(t *testing.T) {
	ctx := context.Background()
	repo := newProvenanceTestRepo(t)
	seedRenameFixture(t, repo)

	view, err := application.NewProvenanceService(repo).Provenance(ctx, "new.go")
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}

	if view.File != "new.go" {
		t.Errorf("view.File = %q, want new.go", view.File)
	}
	if len(view.Rows) != 3 {
		t.Fatalf("got %d rows, want 3 (old.go edit, rename, out-of-band)", len(view.Rows))
	}
	// Rows must be in ascending chain order (seq 1, 2, 3) — proving the pre-rename
	// path's history is folded into the queried path via ResolveRenames.
	for i, want := range []int64{1, 2, 3} {
		if view.Rows[i].Seq != want {
			t.Errorf("row %d seq = %d, want %d", i, view.Rows[i].Seq, want)
		}
	}
}

func TestProvenance_OutOfBandRowFlagged(t *testing.T) {
	ctx := context.Background()
	repo := newProvenanceTestRepo(t)
	seedRenameFixture(t, repo)

	view, err := application.NewProvenanceService(repo).Provenance(ctx, "new.go")
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if len(view.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(view.Rows))
	}

	// The observed edit carries its real source; the out-of-band row is flagged.
	observed := view.Rows[0]
	if observed.Who != string(graph.EventSourceClaudeCode) {
		t.Errorf("observed row Who = %q, want claude-code", observed.Who)
	}
	if observed.OutOfBand {
		t.Error("observed row must not be flagged out-of-band")
	}

	oob := view.Rows[2]
	if oob.Who != string(graph.EventSourceUnknown) {
		t.Errorf("out-of-band row Who = %q, want unknown", oob.Who)
	}
	if !oob.OutOfBand {
		t.Error("out-of-band row must be flagged OutOfBand")
	}
}

func TestProvenance_RowFields(t *testing.T) {
	ctx := context.Background()
	repo := newProvenanceTestRepo(t)
	seedRenameFixture(t, repo)

	view, err := application.NewProvenanceService(repo).Provenance(ctx, "new.go")
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if len(view.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(view.Rows))
	}

	// The first observed change must carry its {when, tool, blob refs, change
	// kind} from the joined events + event_files rows.
	first := view.Rows[0]
	if first.When != 1000 {
		t.Errorf("row 0 When = %d, want 1000", first.When)
	}
	if first.Tool != "Edit" {
		t.Errorf("row 0 Tool = %q, want Edit", first.Tool)
	}
	if first.BeforeBlob != "blob-old-0" || first.AfterBlob != "blob-old-1" {
		t.Errorf("row 0 blob refs = %q->%q, want blob-old-0->blob-old-1",
			first.BeforeBlob, first.AfterBlob)
	}
	if first.ChangeKind != "M" {
		t.Errorf("row 0 ChangeKind = %q, want M", first.ChangeKind)
	}

	// The out-of-band tail row's File Blob Refs come from event_files too.
	last := view.Rows[2]
	if last.BeforeBlob != "blob-new-1" || last.AfterBlob != "blob-new-2" {
		t.Errorf("row 2 blob refs = %q->%q, want blob-new-1->blob-new-2",
			last.BeforeBlob, last.AfterBlob)
	}
}
