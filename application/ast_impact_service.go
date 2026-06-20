package application

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ASTImpactService queries structural (symbol-level) impact from the AST graph.
type ASTImpactService struct {
	repo graph.ASTRepository
}

func NewASTImpactService(repo graph.ASTRepository) *ASTImpactService {
	return &ASTImpactService{repo: repo}
}

// ImpactBySymbol finds callers/references of the named symbol up to maxDepth
// hops. If multiple symbols share the name, all are expanded and their impact
// sets merged.
func (s *ASTImpactService) ImpactBySymbol(ctx context.Context, name string, maxDepth int) (*graph.ASTImpactResult, error) {
	start := time.Now()

	if name == "" {
		return nil, fmt.Errorf("ast impact: symbol name is required")
	}
	if maxDepth <= 0 {
		maxDepth = 1
	}

	nodes, err := s.repo.GetASTNodeByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("lookup symbol %q: %w", name, err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("symbol %q not found", name)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return astNodeSortKey(nodes[i]) < astNodeSortKey(nodes[j])
	})

	merged := map[string]graph.ASTImpactEntry{}
	for _, n := range nodes {
		callers, err := s.repo.GetCallers(ctx, n.ID, maxDepth)
		if err != nil {
			return nil, fmt.Errorf("get callers for %s: %w", n.ID, err)
		}
		for _, e := range callers {
			// Prefer the shallowest depth across same-named seeds: a caller
			// reached at depth 1 from one seed should not be reported at depth 2
			// just because an earlier seed (sorted by qualified name) found it
			// deeper.
			if existing, ok := merged[e.Node.ID]; !ok || e.Depth < existing.Depth {
				merged[e.Node.ID] = e
			}
		}
	}

	result := &graph.ASTImpactResult{
		SeedNode:   nodes[0],
		Impacted:   make([]graph.ASTImpactEntry, 0, len(merged)),
		TotalFound: len(merged),
		QueryMs:    time.Since(start).Milliseconds(),
	}
	for _, e := range merged {
		result.Impacted = append(result.Impacted, e)
	}
	sort.Slice(result.Impacted, func(i, j int) bool {
		a, b := result.Impacted[i], result.Impacted[j]
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		return astNodeSortKey(a.Node) < astNodeSortKey(b.Node)
	})
	return result, nil
}

func astNodeSortKey(n graph.ASTNode) string {
	if n.QualifiedName != "" {
		return n.QualifiedName
	}
	if n.FilePath != "" {
		return n.FilePath + "::" + n.Name + "::" + n.ID
	}
	return n.Name + "::" + n.ID
}

// Search finds symbols by name prefix, optionally filtered by kind.
func (s *ASTImpactService) Search(ctx context.Context, query string, kinds []graph.ASTNodeKind) ([]graph.ASTSearchResult, error) {
	return s.repo.SearchASTNodes(ctx, query, kinds)
}
