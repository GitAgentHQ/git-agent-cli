package application

import (
	"context"
	"fmt"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// EnsureIndexService decides whether to run a full or incremental index
// based on the current state of the graph database.
type EnsureIndexService struct {
	indexSvc *IndexService
	repo     graph.GraphRepository
	git      graph.GraphGitClient
}

// NewEnsureIndexService creates an EnsureIndexService.
func NewEnsureIndexService(indexSvc *IndexService, repo graph.GraphRepository, git graph.GraphGitClient, dbPath string) *EnsureIndexService {
	return &EnsureIndexService{
		indexSvc: indexSvc,
		repo:     repo,
		git:      git,
	}
}

// EnsureIndex checks the current index state and runs the appropriate
// indexing strategy: full re-index or incremental.
func (s *EnsureIndexService) EnsureIndex(ctx context.Context, req graph.IndexRequest) (*graph.IndexResult, error) {
	lastHash, err := s.repo.GetLastIndexedCommit(ctx)
	if err != nil {
		return nil, fmt.Errorf("get last indexed commit: %w", err)
	}

	if lastHash == "" {
		return s.fullIndex(ctx, req)
	}

	if req.Force {
		return s.fullIndex(ctx, req)
	}

	reachable, err := s.git.MergeBaseIsAncestor(ctx, lastHash, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("check reachability: %w", err)
	}

	if !reachable {
		return s.fullIndex(ctx, req)
	}

	return s.indexSvc.IncrementalIndex(ctx, lastHash, req)
}

func (s *EnsureIndexService) fullIndex(ctx context.Context, req graph.IndexRequest) (*graph.IndexResult, error) {
	// Reset commit-derived graph data without deleting user capture history.
	if err := s.repo.ResetIndexData(ctx); err != nil {
		return nil, fmt.Errorf("reset graph index: %w", err)
	}
	return s.indexSvc.FullIndex(ctx, req)
}
