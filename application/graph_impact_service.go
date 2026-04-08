package application

import (
	"context"
	"fmt"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ImpactService queries co-change impact for a given file.
type ImpactService struct {
	repo graph.GraphRepository
}

// NewImpactService creates an ImpactService with the given repository.
func NewImpactService(repo graph.GraphRepository) *ImpactService {
	return &ImpactService{repo: repo}
}

// Impact returns the co-changed files for the given path, applying defaults
// and validation before delegating to the repository.
func (s *ImpactService) Impact(ctx context.Context, req graph.ImpactRequest) (*graph.ImpactResult, error) {
	if req.Path == "" {
		return nil, fmt.Errorf("impact: path must not be empty")
	}

	if req.Depth <= 0 {
		req.Depth = 1
	}
	if req.Top <= 0 {
		req.Top = 20
	}
	if req.MinCount <= 0 {
		req.MinCount = 3
	}

	start := time.Now()
	result, err := s.repo.Impact(ctx, req)
	if err != nil {
		return nil, err
	}
	result.QueryMs = time.Since(start).Milliseconds()
	return result, nil
}
