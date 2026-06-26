package application

import (
	"context"
	"fmt"
	"sort"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// AffectedEntry is a test-file symbol that depends on a changed symbol.
type AffectedEntry struct {
	TestFile string `json:"test_file"`
	Symbol   string `json:"symbol"`
	Kind     string `json:"kind"`
	Line     int    `json:"line"`
	Depth    int    `json:"depth"`
	Via      string `json:"via"` // changed symbol that links this test symbol
}

// AffectedResult is the set of test symbols transitively affected by the
// changed files.
type AffectedResult struct {
	ChangedFiles []string        `json:"changed_files"`
	Tests        []AffectedEntry `json:"tests"`
	Total        int             `json:"total"`
}

// AffectedService traces transitive dependents of changed files' symbols and
// filters them to test files. It is the codegraph `affected` analogue over the
// AST call graph.
type AffectedService struct {
	astRepo graph.ASTRepository
}

func NewAffectedService(astRepo graph.ASTRepository) *AffectedService {
	return &AffectedService{astRepo: astRepo}
}

// Affected returns the test symbols that transitively call or reference any
// symbol declared in the changed files, walked up to maxDepth.
func (s *AffectedService) Affected(ctx context.Context, changedFiles []string, maxDepth int) (*AffectedResult, error) {
	if maxDepth <= 0 {
		maxDepth = 2
	}
	res := &AffectedResult{ChangedFiles: changedFiles}
	seen := make(map[string]bool)
	for _, file := range changedFiles {
		nodes, err := s.astRepo.ListASTNodesByFile(ctx, file)
		if err != nil {
			return nil, fmt.Errorf("list nodes in %s: %w", file, err)
		}
		for _, n := range nodes {
			callers, err := s.astRepo.GetCallers(ctx, n.ID, maxDepth)
			if err != nil {
				return nil, fmt.Errorf("callers of %s: %w", n.ID, err)
			}
			for _, c := range callers {
				if !graph.IsTestFile(c.Node.FilePath) {
					continue
				}
				key := fmt.Sprintf("%s:%s:%d", c.Node.FilePath, c.Node.ID, c.Depth)
				if seen[key] {
					continue
				}
				seen[key] = true
				res.Tests = append(res.Tests, AffectedEntry{
					TestFile: c.Node.FilePath,
					Symbol:   c.Node.Name,
					Kind:     string(c.Node.Kind),
					Line:     c.Node.StartLine,
					Depth:    c.Depth,
					Via:      n.Name,
				})
			}
		}
	}
	sort.SliceStable(res.Tests, func(i, j int) bool {
		if res.Tests[i].TestFile != res.Tests[j].TestFile {
			return res.Tests[i].TestFile < res.Tests[j].TestFile
		}
		return res.Tests[i].Line < res.Tests[j].Line
	})
	res.Total = len(res.Tests)
	return res, nil
}
