package application

import (
	"context"
	"fmt"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ImpactService queries co-change impact for one or more seed files.
type ImpactService struct {
	repo graph.GraphRepository
}

// NewImpactService creates an ImpactService with the given repository.
func NewImpactService(repo graph.GraphRepository) *ImpactService {
	return &ImpactService{repo: repo}
}

// Impact returns the aggregated co-change neighbours of the seed paths, applying
// defaults and validation before delegating to the repository. Blank seeds are
// dropped; at least one non-empty seed is required.
func (s *ImpactService) Impact(ctx context.Context, req graph.ImpactRequest) (*graph.ImpactResult, error) {
	seeds := make([]string, 0, len(req.Paths))
	for _, p := range req.Paths {
		if p != "" {
			seeds = append(seeds, p)
		}
	}
	if len(seeds) == 0 {
		return nil, fmt.Errorf("impact: at least one seed path is required")
	}
	req.Paths = seeds

	if req.Depth <= 0 {
		req.Depth = 1
	}
	if req.Top <= 0 {
		req.Top = 20
	}
	if req.MinCount <= 0 {
		req.MinCount = 3
	}

	result, err := s.repo.Impact(ctx, req)
	if err != nil {
		return nil, err
	}
	return result, nil
}
