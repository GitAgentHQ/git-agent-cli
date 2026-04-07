package graph

import "context"

// GraphGitClient defines git operations needed for graph indexing and capture.
type GraphGitClient interface {
	CommitLogDetailed(ctx context.Context, sinceHash string, maxCommits int) ([]CommitInfo, error)
	CurrentHead(ctx context.Context) (string, error)
	MergeBaseIsAncestor(ctx context.Context, ancestor, head string) (bool, error)
	HashObject(ctx context.Context, filePath string) (string, error)
	DiffNameOnly(ctx context.Context) ([]string, error)
	DiffForFiles(ctx context.Context, files []string) (string, error)
}
