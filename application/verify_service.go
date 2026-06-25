package application

import (
	"context"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// VerifyService orchestrates the Event Log chain integrity check. The walk and
// break classification live in the repository (infrastructure); this layer only
// runs it and hands the result to the command layer.
type VerifyService struct {
	repo graph.GraphRepository
}

// NewVerifyService creates a VerifyService over the given repository.
func NewVerifyService(repo graph.GraphRepository) *VerifyService {
	return &VerifyService{repo: repo}
}

// Verify runs the chain integrity check and returns the result.
func (s *VerifyService) Verify(ctx context.Context) (graph.VerifyResult, error) {
	return s.repo.VerifyChain(ctx)
}
