package application

import (
	"context"
	"fmt"
	"os"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// EnsureIndexService decides whether to run a full or incremental index
// based on the current state of the graph database.
type EnsureIndexService struct {
	indexSvc *IndexService
	repo     graph.GraphRepository
	git      graph.GraphGitClient
	dbPath   string
}

// NewEnsureIndexService creates an EnsureIndexService.
func NewEnsureIndexService(indexSvc *IndexService, repo graph.GraphRepository, git graph.GraphGitClient, dbPath string) *EnsureIndexService {
	return &EnsureIndexService{
		indexSvc: indexSvc,
		repo:     repo,
		git:      git,
		dbPath:   dbPath,
	}
}

// EnsureIndex checks the current index state and runs the appropriate
// indexing strategy: full re-index or incremental.
func (s *EnsureIndexService) EnsureIndex(ctx context.Context, req graph.IndexRequest) (*graph.IndexResult, error) {
	// 1. Check if DB file exists. If not, run full index.
	if _, err := os.Stat(s.dbPath); os.IsNotExist(err) {
		return s.fullIndex(ctx, req)
	}

	// 2. Get lastHash from the repository.
	lastHash, err := s.repo.GetLastIndexedCommit(ctx)
	if err != nil {
		return nil, fmt.Errorf("get last indexed commit: %w", err)
	}

	// 3. If lastHash is empty, run full index.
	if lastHash == "" {
		return s.fullIndex(ctx, req)
	}

	// 4. If Force flag is set, run full index.
	if req.Force {
		return s.fullIndex(ctx, req)
	}

	// 5. Check reachability of the last indexed commit.
	reachable, err := s.git.MergeBaseIsAncestor(ctx, lastHash, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("check reachability: %w", err)
	}

	// 6. If not reachable (force-push recovery), run full index.
	if !reachable {
		fmt.Fprintf(os.Stderr, "warning: last indexed commit %s is no longer reachable, running full re-index\n", lastHash)
		return s.fullIndex(ctx, req)
	}

	// 7. Otherwise, run incremental index.
	return s.indexSvc.IncrementalIndex(ctx, lastHash, req)
}

func (s *EnsureIndexService) fullIndex(ctx context.Context, req graph.IndexRequest) (*graph.IndexResult, error) {
	// Drop and recreate the database so stale history from force-pushes or
	// rebases doesn't pollute co-change results.
	if err := s.repo.Drop(ctx); err != nil {
		return nil, fmt.Errorf("drop graph db: %w", err)
	}
	if err := s.repo.Open(ctx); err != nil {
		return nil, fmt.Errorf("reopen graph db: %w", err)
	}
	if err := s.repo.InitSchema(ctx); err != nil {
		return nil, fmt.Errorf("reinit schema: %w", err)
	}
	return s.indexSvc.FullIndex(ctx, req)
}
