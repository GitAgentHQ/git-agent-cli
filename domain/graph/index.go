package graph

// IndexRequest controls how indexing is performed.
type IndexRequest struct {
	Force             bool // force full re-index
	MaxCommits        int  // 0 = unlimited
	MaxFilesPerCommit int  // skip commits touching more files (default 50)
	CoChangeThreshold int  // full co-change recompute threshold (default 500)
}

// IndexResult reports what was indexed.
type IndexResult struct {
	IndexedCommits int    `json:"indexed_commits"`
	NewCommits     int    `json:"new_commits"`
	Files          int    `json:"files"`
	Authors        int    `json:"authors"`
	DurationMs     int64  `json:"duration_ms"`
	LastCommit     string `json:"last_commit"`
}
