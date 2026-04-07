package graph

import "context"

// GraphRepository defines all persistence operations for the code graph.
type GraphRepository interface {
	// Lifecycle
	Open(ctx context.Context) error
	Close() error
	InitSchema(ctx context.Context) error
	Drop(ctx context.Context) error

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
	CreateActionModifies(ctx context.Context, actionID, filePath string, additions, deletions int) error
	CreateActionProduces(ctx context.Context, actionID, commitHash string) error
	Timeline(ctx context.Context, req TimelineRequest) (*TimelineResult, error)
	UnlinkedActionsForFiles(ctx context.Context, filePaths []string, since int64) ([]ActionNode, error)

	// Capture baseline
	GetCaptureBaseline(ctx context.Context, filePaths []string) (map[string]string, error)
	UpdateCaptureBaseline(ctx context.Context, updates map[string]string) error
	CleanupCaptureBaseline(ctx context.Context, currentFiles []string, olderThan int64) error

	// Stats
	GetStats(ctx context.Context) (*GraphStats, error)
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
