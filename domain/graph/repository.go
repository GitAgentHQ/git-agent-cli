package graph

import "context"

// ProjectionHighWaterKey is the index_state key holding the highest Event Log
// seq folded into the derived Projections. It is the staleness check's projected
// high-water mark, stored explicitly because not every Event kind produces an
// event_files row (Outcome Events touch no files), so the row count cannot stand
// in for how far the Replay has progressed.
const ProjectionHighWaterKey = "max_projected_event_seq"

// GraphRepository defines all persistence operations for the code graph.
type GraphRepository interface {
	// Lifecycle
	Open(ctx context.Context) error
	Close() error
	InitSchema(ctx context.Context) error
	Drop(ctx context.Context) error
	ResetIndexData(ctx context.Context) error

	// Indexing writes
	UpsertCommit(ctx context.Context, c CommitNode) error
	UpsertAuthor(ctx context.Context, a AuthorNode) error
	UpsertFile(ctx context.Context, f FileNode) error
	CreateModifies(ctx context.Context, e ModifiesEdge) error
	CreateAuthored(ctx context.Context, authorEmail, commitHash string) error
	CreateRename(ctx context.Context, oldPath, newPath, commitHash string) error

	// Co-change
	RecomputeCoChanged(ctx context.Context, minCount, maxFilesPerCommit int) error
	IncrementalCoChanged(ctx context.Context, touchedFiles []string, minCount, maxFilesPerCommit int) error

	// Index state
	GetIndexState(ctx context.Context, key string) (string, error)
	SetIndexState(ctx context.Context, key, value string) error
	GetLastIndexedCommit(ctx context.Context) (string, error)
	SetLastIndexedCommit(ctx context.Context, hash string) error
	GetSchemaVersion(ctx context.Context) (int, error)
	SetSchemaVersion(ctx context.Context, version int) error

	// Queries
	Impact(ctx context.Context, req ImpactRequest) (*ImpactResult, error)
	ResolveRenames(ctx context.Context, filePath string) ([]string, error)
	// LinkingCommits returns the commits that modified both seed and related
	// (the commits that bind a co-change pair), most-recent first, capped at
	// limit. It is the "why are these related?" evidence behind a co-change edge.
	LinkingCommits(ctx context.Context, seed, related string, limit int) ([]CommitRef, error)

	// Session/Action tracking
	GetActiveSession(ctx context.Context, source, instanceID string, timeoutMinutes int) (*SessionNode, error)
	UpsertSession(ctx context.Context, s SessionNode) error
	EndSession(ctx context.Context, sessionID string) error
	CreateAction(ctx context.Context, a ActionNode) error
	// CreateActionBatch atomically derives the action's sequence and id, then
	// creates the action and its modifies edges in a single transaction. The
	// caller leaves a.ID and a.Sequence unset; both are assigned inside the
	// transaction (from the session's current max sequence) so concurrent
	// captures on the same session can't collide on the action id. The persisted
	// action, with ID and Sequence populated, is returned. Each FileChange
	// carries the per-file addition and deletion counts for the action.
	CreateActionBatch(ctx context.Context, a ActionNode, modifiedFiles []FileChange) (ActionNode, error)
	GetActionCountForSession(ctx context.Context, sessionID string) (int, error)
	CreateActionModifies(ctx context.Context, actionID, filePath string, additions, deletions int) error
	CreateActionProduces(ctx context.Context, actionID, commitHash, filePath string) error
	Timeline(ctx context.Context, req TimelineRequest) (*TimelineResult, error)
	UnlinkedActionsForFiles(ctx context.Context, filePaths []string, since int64) ([]ActionNode, error)

	// Event Log (append-only chain). AppendEvent is the only writer into events;
	// it assigns seq and this_hash inside a single BEGIN IMMEDIATE transaction.
	AppendEvent(ctx context.Context, e EventRecord) (EventRecord, error)
	HeadHash(ctx context.Context) (string, error)
	// MaxEventSeq returns the highest seq in the Event Log (0 when empty).
	MaxEventSeq(ctx context.Context) (int64, error)
	// MaxProjectedEventSeq returns the highest Event Log seq folded into the
	// derived Projections (0 when none), read from the explicit high-water mark
	// (ProjectionHighWaterKey) the Replay records. Equal to MaxEventSeq, the cold
	// path is current — the basis of sync's staleness check.
	MaxProjectedEventSeq(ctx context.Context) (int64, error)
	StreamEvents(ctx context.Context, sinceSeq int64) (EventCursor, error)
	// VerifyChain walks the Event Log, recomputes each this_hash, follows the
	// genesis prev_hash linkage, and checks seq continuity to classify the first
	// integrity break. Read-only; safe under WAL alongside an active writer.
	VerifyChain(ctx context.Context) (VerifyResult, error)
	// ResetProjections truncates the derived (Projection) tables — sessions,
	// actions, action_modifies, action_produces, event_files — so a rebuild can
	// regenerate them from the Event Log. The append-only events table is never
	// touched.
	ResetProjections(ctx context.Context) error
	// CreateEventFile records one touched-file row for an Event (the File Blob
	// Refs derived on the cold path). event_seq + file_path is the key.
	CreateEventFile(ctx context.Context, ef EventFile) error
	// LastEventFileSeqForPath returns the highest event_seq currently projected
	// for filePath (0 when none). Used by incremental sync to find the row whose
	// after_blob was git-derived as the path's final touch.
	LastEventFileSeqForPath(ctx context.Context, filePath string) (int64, error)
	// ClearEventFileAfterBlob sets after_blob to empty for one (filePath, eventSeq)
	// row. Incremental sync clears a superseded final touch so its after_blob no
	// longer claims to be the path's current state.
	ClearEventFileAfterBlob(ctx context.Context, filePath string, eventSeq int64) error
	// LoadOpenSession returns the latest session for (source, instanceID) with
	// its last action's timestamp and next sequence number, so incremental sync
	// can extend an existing session or open a new one by inter-event gap.
	// Returns ("", 0, 0) when no session exists for the key.
	LoadOpenSession(ctx context.Context, source, instanceID string) (sessionID string, lastAt int64, nextSeq int, err error)
	// FileChanges returns the event_files rows for any of filePaths, joined to
	// their events row (and any action_produces linked commit), in ascending seq
	// order. It is the read behind graph provenance and the diagnose Candidate
	// blob refs; observed and out-of-band Events share the events table, so one
	// ordered read covers both.
	FileChanges(ctx context.Context, filePaths []string) ([]FileChangeRow, error)
	// LatestAfterBlob returns the most recent (highest event_seq) after_blob
	// recorded for filePath in event_files — the last content state the Event Log
	// accounts for. ok is false when the log has never touched the path.
	LatestAfterBlob(ctx context.Context, filePath string) (blob string, ok bool, err error)

	// Stats
	GetStats(ctx context.Context) (*GraphStats, error)
}

// GraphStats holds counts for the `status` command display.
type GraphStats struct {
	Exists            bool   `json:"exists"`
	LastIndexedCommit string `json:"last_indexed_commit,omitempty"`
	CommitCount       int    `json:"commit_count"`
	FileCount         int    `json:"file_count"`
	AuthorCount       int    `json:"author_count"`
	CoChangedCount    int    `json:"co_changed_count"`
	SessionCount      int    `json:"session_count"`
	ActionCount       int    `json:"action_count"`
	DBSizeBytes       int64  `json:"db_size_bytes"`
}
