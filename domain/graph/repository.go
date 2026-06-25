package graph

import "context"

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
	// MaxProjectedEventSeq returns the highest event_seq reflected in the
	// event_files projection (0 when empty). Equal to MaxEventSeq, the cold
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

// ASTRepository defines persistence operations for AST-extracted code symbols.
type ASTRepository interface {
	UpsertASTNode(ctx context.Context, n ASTNode) error
	UpsertASTEdge(ctx context.Context, e ASTEdge) error
	UpsertUnresolvedRef(ctx context.Context, ref ASTUnresolvedRef) error
	GetASTNodeByName(ctx context.Context, name string) ([]ASTNode, error)
	GetASTNodeByQualifiedName(ctx context.Context, qname string) (*ASTNode, error)
	ListASTNodesByFile(ctx context.Context, filePath string) ([]ASTNode, error)
	GetCallers(ctx context.Context, nodeID string, maxDepth int) ([]ASTImpactEntry, error)
	GetCallees(ctx context.Context, nodeID string, maxDepth int) ([]ASTImpactEntry, error)
	GetImpactRadius(ctx context.Context, nodeID string, maxDepth int) (*ASTImpactResult, error)
	SearchASTNodes(ctx context.Context, query string, kinds []ASTNodeKind) ([]ASTSearchResult, error)
	ListUnresolvedRefs(ctx context.Context) ([]ASTUnresolvedRef, error)
	// ListUnresolvedRefsMatching returns refs in any of filePaths or whose
	// trailing symbol name (after the last '.') matches lookupNames. When both
	// slices are empty, all unresolved refs are returned.
	ListUnresolvedRefsMatching(ctx context.Context, filePaths []string, lookupNames []string) ([]ASTUnresolvedRef, error)
	ListASTNodeNames(ctx context.Context) ([]string, error)
	DeleteASTNodesForFile(ctx context.Context, filePath string) error
	DeleteASTNodesExceptFiles(ctx context.Context, filePaths []string) error
}

// GraphStats holds counts for graph status display.
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
