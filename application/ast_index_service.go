package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gitagenthq/git-agent/domain/extraction"
	"github.com/gitagenthq/git-agent/domain/graph"
)

// ASTIndexResult holds the output of an AST indexing pass.
type ASTIndexResult struct {
	FilesProcessed     int
	SymbolsStored      int
	EdgesStored        int
	ResolvedRefs       int
	AmbiguousRefs      int
	UnresolvedNotFound int
	DurationMs         int64
}

// ASTIndexService orchestrates extracting AST symbols from source files and
// persisting them into the AST graph repository. It walks the git-tracked files
// of a repo (filtered by supported extensions), extracts symbols via tree-sitter,
// and upserts them.
type ASTIndexService struct {
	astRepo   graph.ASTRepository
	git       TrackedFileLister
	extractor extraction.SymbolExtractor
}

type TrackedFileLister interface {
	TrackedFiles(ctx context.Context, pathspec string) ([]string, error)
}

type astTransactioner interface {
	RunInTx(ctx context.Context, fn func() error) error
}

func NewASTIndexService(astRepo graph.ASTRepository, git TrackedFileLister, extractor extraction.SymbolExtractor) *ASTIndexService {
	return &ASTIndexService{astRepo: astRepo, git: git, extractor: extractor}
}

// IndexAll walks all tracked Go files, extracts symbols, and persists them.
func (s *ASTIndexService) IndexAll(ctx context.Context, root string) (*ASTIndexResult, error) {
	start := time.Now()

	goFiles, err := s.listTrackedGoFiles(ctx)
	if err != nil {
		return nil, err
	}

	result := &ASTIndexer{astRepo: s.astRepo, extractor: s.extractor}
	var resResult *ReferenceResolverResult
	if err := runASTTransaction(ctx, s.astRepo, func() error {
		if err := s.astRepo.DeleteASTNodesExceptFiles(ctx, goFiles); err != nil {
			return fmt.Errorf("clear stale ast data: %w", err)
		}
		for _, f := range goFiles {
			ext, err := s.extractFile(root, f)
			if err != nil {
				return err
			}
			if err := result.upsertFile(ctx, f, ext); err != nil {
				return fmt.Errorf("upsert %s: %w", f, err)
			}
		}
		resolver := NewReferenceResolver(s.astRepo, nil)
		var err error
		resResult, err = resolver.Resolve(ctx)
		if err != nil {
			return fmt.Errorf("resolve references: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return s.buildIndexResult(result, resResult, start), nil
}

// IndexFiles re-indexes the given repo-relative paths and optionally prunes AST
// nodes for tracked Go files that are no longer present.
func (s *ASTIndexService) IndexFiles(ctx context.Context, root string, relPaths []string, pruneToTracked bool) (*ASTIndexResult, error) {
	start := time.Now()

	paths := dedupeGoPaths(relPaths)
	if len(paths) == 0 {
		if !pruneToTracked {
			return &ASTIndexResult{DurationMs: time.Since(start).Milliseconds()}, nil
		}
		trackedGoFiles, err := s.listTrackedGoFiles(ctx)
		if err != nil {
			return nil, err
		}
		if err := runASTTransaction(ctx, s.astRepo, func() error {
			return s.astRepo.DeleteASTNodesExceptFiles(ctx, trackedGoFiles)
		}); err != nil {
			return nil, fmt.Errorf("prune stale ast data: %w", err)
		}
		return &ASTIndexResult{DurationMs: time.Since(start).Milliseconds()}, nil
	}

	type extractedFile struct {
		path string
		res  *graph.ExtractionResult
	}
	extracted := make([]extractedFile, 0, len(paths))
	var lookupNames []string
	for _, f := range paths {
		ext, err := s.extractFile(root, f)
		if err != nil {
			return nil, err
		}
		extracted = append(extracted, extractedFile{path: f, res: ext})
		lookupNames = mergeUniqueStrings(lookupNames, symbolNamesFrom(ext))
	}

	var trackedGoFiles []string
	if pruneToTracked {
		var err error
		trackedGoFiles, err = s.listTrackedGoFiles(ctx)
		if err != nil {
			return nil, err
		}
	}

	result := &ASTIndexer{astRepo: s.astRepo, extractor: s.extractor}
	var resResult *ReferenceResolverResult
	if err := runASTTransaction(ctx, s.astRepo, func() error {
		if pruneToTracked {
			if err := s.astRepo.DeleteASTNodesExceptFiles(ctx, trackedGoFiles); err != nil {
				return fmt.Errorf("prune stale ast data: %w", err)
			}
		}
		resolvePaths := make([]string, 0, len(extracted))
		for _, f := range extracted {
			if err := s.astRepo.DeleteASTNodesForFile(ctx, f.path); err != nil {
				return fmt.Errorf("delete old nodes for %s: %w", f.path, err)
			}
			if err := result.upsertFile(ctx, f.path, f.res); err != nil {
				return fmt.Errorf("upsert %s: %w", f.path, err)
			}
			resolvePaths = append(resolvePaths, f.path)
		}
		resolver := NewReferenceResolver(s.astRepo, nil)
		var err error
		resResult, err = resolver.ResolveForFilesAndNames(ctx, resolvePaths, lookupNames)
		if err != nil {
			return fmt.Errorf("resolve references: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return s.buildIndexResult(result, resResult, start), nil
}

// IndexFile re-indexes a single file: deletes its existing AST nodes, re-extracts,
// and upserts the new nodes.
func (s *ASTIndexService) IndexFile(ctx context.Context, root, relPath string) (*ASTIndexResult, error) {
	if !isGoFile(relPath) {
		start := time.Now()
		if err := runASTTransaction(ctx, s.astRepo, func() error {
			return s.astRepo.DeleteASTNodesForFile(ctx, relPath)
		}); err != nil {
			return nil, fmt.Errorf("delete old nodes for %s: %w", relPath, err)
		}
		return &ASTIndexResult{DurationMs: time.Since(start).Milliseconds()}, nil
	}
	return s.IndexFiles(ctx, root, []string{relPath}, false)
}

func (s *ASTIndexService) listTrackedGoFiles(ctx context.Context) ([]string, error) {
	files, err := s.git.TrackedFiles(ctx, ".")
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}
	return filterGoFiles(files), nil
}

func (s *ASTIndexService) extractFile(root, relPath string) (*graph.ExtractionResult, error) {
	abs := filepath.Join(root, relPath)
	source, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", abs, err)
	}
	ext, err := s.extractor.Extract(relPath, source)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", relPath, err)
	}
	return ext, nil
}

func (s *ASTIndexService) buildIndexResult(result *ASTIndexer, resResult *ReferenceResolverResult, start time.Time) *ASTIndexResult {
	out := &ASTIndexResult{
		FilesProcessed: result.filesProcessed,
		SymbolsStored:  result.symbolsStored,
		EdgesStored:    result.edgesStored,
		DurationMs:     time.Since(start).Milliseconds(),
	}
	if resResult != nil {
		out.ResolvedRefs = resResult.ResolvedCount
		out.AmbiguousRefs = resResult.AmbiguousCount
		out.UnresolvedNotFound = resResult.NotFoundCount
	}
	return out
}

func runASTTransaction(ctx context.Context, repo graph.ASTRepository, fn func() error) error {
	if txRepo, ok := repo.(astTransactioner); ok {
		return txRepo.RunInTx(ctx, fn)
	}
	return fn()
}

// ASTIndexer is an internal helper that accumulates upserts and counts.
type ASTIndexer struct {
	astRepo        graph.ASTRepository
	extractor      extraction.SymbolExtractor
	filesProcessed int
	symbolsStored  int
	edgesStored    int
}

func (r *ASTIndexer) upsertFile(ctx context.Context, relPath string, ext *graph.ExtractionResult) error {
	for _, n := range ext.Nodes {
		n.FilePath = relPath
		if n.UpdatedAt == 0 {
			n.UpdatedAt = time.Now().Unix()
		}
		if err := r.astRepo.UpsertASTNode(ctx, n); err != nil {
			return err
		}
		r.symbolsStored++
	}

	for _, e := range ext.Edges {
		if err := r.astRepo.UpsertASTEdge(ctx, e); err != nil {
			return err
		}
		r.edgesStored++
	}

	for _, ref := range ext.UnresolvedRefs {
		if err := r.astRepo.UpsertUnresolvedRef(ctx, ref); err != nil {
			return err
		}
	}

	r.filesProcessed++
	return nil
}

func filterGoFiles(files []string) []string {
	var goFiles []string
	for _, f := range files {
		if isGoFile(f) && !graph.IsToolingPath(f) {
			goFiles = append(goFiles, f)
		}
	}
	return goFiles
}

func dedupeGoPaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	var out []string
	for _, p := range paths {
		if !isGoFile(p) || graph.IsToolingPath(p) || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func symbolNamesFrom(ext *graph.ExtractionResult) []string {
	if ext == nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	for _, n := range ext.Nodes {
		if n.Name == "" || seen[n.Name] {
			continue
		}
		seen[n.Name] = true
		names = append(names, n.Name)
	}
	return names
}

func mergeUniqueStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, items := range [][]string{a, b} {
		for _, s := range items {
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func isGoFile(p string) bool {
	return strings.HasSuffix(p, ".go")
}
