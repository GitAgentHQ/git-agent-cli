package application

import (
	"context"
	"fmt"
	"sort"

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
	if req.IncludeCommits {
		if err := s.enrichLinkingCommits(ctx, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// maxLinkingCommits caps how many linking commits are attached per related file.
const maxLinkingCommits = 3

// enrichLinkingCommits attaches, to each result entry, the commits that bind it
// to its seed(s) — the "why are these related?" evidence. It runs only on the
// already-truncated result set (one bounded JOIN per related/seed pair), so the
// cost scales with Top, not with history size. A file coupled to several seeds
// unions their linking commits, deduped by hash and kept most-recent first.
func (s *ImpactService) enrichLinkingCommits(ctx context.Context, result *graph.ImpactResult) error {
	if result == nil {
		return nil
	}
	for i := range result.CoChanged {
		e := &result.CoChanged[i]
		seeds := e.RelatedTo
		if len(seeds) == 0 {
			seeds = result.Targets
		}
		seen := make(map[string]bool)
		var commits []graph.CommitRef
		for _, seed := range seeds {
			refs, err := s.repo.LinkingCommits(ctx, seed, e.Path, maxLinkingCommits)
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if seen[ref.Hash] {
					continue
				}
				seen[ref.Hash] = true
				commits = append(commits, ref)
			}
		}
		sort.Slice(commits, func(a, b int) bool {
			return commits[a].Timestamp > commits[b].Timestamp
		})
		if len(commits) > maxLinkingCommits {
			commits = commits[:maxLinkingCommits]
		}
		e.LinkingCommits = commits
	}
	return nil
}
