package graph

// CommitNode represents a single git commit in the graph.
type CommitNode struct {
	Hash         string
	Message      string
	AuthorName   string
	AuthorEmail  string
	Timestamp    int64
	ParentHashes []string
}

// FileNode represents a tracked file.
type FileNode struct {
	Path            string
	Language        string
	LastIndexedHash string
}

// AuthorNode represents a commit author.
type AuthorNode struct {
	Email string
	Name  string
}

// ModifiesEdge links a commit to a file it modified.
type ModifiesEdge struct {
	CommitHash string
	FilePath   string
	Additions  int
	Deletions  int
	Status     string // A (added), M (modified), D (deleted), R (renamed)
}

// CoChangedEntry represents a co-change relationship between two files.
type CoChangedEntry struct {
	FileA            string
	FileB            string
	CouplingCount    int
	CouplingStrength float64
	LastCoupledHash  string
}

// RenameEntry tracks a file rename across commits.
type RenameEntry struct {
	OldPath    string
	NewPath    string
	CommitHash string
}

// CommitInfo is the structured output of a detailed git log entry,
// used during indexing.
type CommitInfo struct {
	Hash         string
	Message      string
	AuthorName   string
	AuthorEmail  string
	Timestamp    int64
	ParentHashes []string
	Files        []FileChange
}

// FileChange describes a single file modification within a commit.
type FileChange struct {
	Path      string
	OldPath   string // populated for renames
	Status    string // A, M, D, R
	Additions int
	Deletions int
}
