package application

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// Affected returns the test symbols that should be re-run for the changed files:
//   - test functions declared in a changed test file (you edited the test, run it),
//     attributed at depth 0; and
//   - test functions that transitively call or reference any symbol declared in
//     the changed files, walked up to maxDepth.
//
// A symbol qualifying both ways is listed once, preferring the depth-0 "you
// changed this test" attribution.
func (s *AffectedService) Affected(ctx context.Context, changedFiles []string, maxDepth int) (*AffectedResult, error) {
	if maxDepth <= 0 {
		maxDepth = 2
	}
	res := &AffectedResult{ChangedFiles: changedFiles}
	// seen dedupes by (test file, symbol id) across both passes so a test that is
	// both edited and a transitive caller appears once.
	seen := make(map[string]bool)
	add := func(e AffectedEntry, symbolID string) {
		key := e.TestFile + "\x00" + symbolID
		if seen[key] {
			return
		}
		seen[key] = true
		res.Tests = append(res.Tests, e)
	}

	// Pass 1: a changed test file's own test functions must be re-run directly —
	// a Go test function has no callers, so the transitive walk never reaches it.
	for _, file := range changedFiles {
		if !graph.IsTestFile(file) {
			continue
		}
		nodes, err := s.astRepo.ListASTNodesByFile(ctx, file)
		if err != nil {
			return nil, fmt.Errorf("list nodes in %s: %w", file, err)
		}
		for _, n := range nodes {
			if !isGoTestFunc(n) {
				continue
			}
			add(AffectedEntry{
				TestFile: file,
				Symbol:   n.Name,
				Kind:     string(n.Kind),
				Line:     n.StartLine,
				Depth:    0,
				Via:      n.Name,
			}, n.ID)
		}
	}

	// Pass 2: test functions that transitively depend on a changed symbol.
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
				add(AffectedEntry{
					TestFile: c.Node.FilePath,
					Symbol:   c.Node.Name,
					Kind:     string(c.Node.Kind),
					Line:     c.Node.StartLine,
					Depth:    c.Depth,
					Via:      n.Name,
				}, c.Node.ID)
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

// isGoTestFunc reports whether n is a Go test entry point (a top-level function
// named Test/Benchmark/Example/Fuzz...), the unit `go test` runs.
func isGoTestFunc(n graph.ASTNode) bool {
	if n.Kind != graph.ASTNodeKindFunction {
		return false
	}
	return strings.HasPrefix(n.Name, "Test") ||
		strings.HasPrefix(n.Name, "Benchmark") ||
		strings.HasPrefix(n.Name, "Example") ||
		strings.HasPrefix(n.Name, "Fuzz")
}
