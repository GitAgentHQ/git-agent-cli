package graph

import "context"

// GraphRepository defines all persistence operations for the code graph.
// The graph is a single data source: commit-history co-change. There is no
// agent Event Log, no sessions/actions, no projections — every table here is
// derived from git history and re-derivable from the current repo.
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
	DBSizeBytes       int64  `json:"db_size_bytes"`
}
