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

	files, err := s.git.TrackedFiles(ctx, ".")
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}

	var goFiles []string
	for _, f := range files {
		if isGoFile(f) && !graph.IsToolingPath(f) {
			goFiles = append(goFiles, f)
		}
	}

	type extractedFile struct {
		path string
		res  *graph.ExtractionResult
	}
	extracted := make([]extractedFile, 0, len(goFiles))
	for _, f := range goFiles {
		abs := filepath.Join(root, f)
		source, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", abs, err)
		}
		extraction, err := s.extractor.Extract(f, source)
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", f, err)
		}
		extracted = append(extracted, extractedFile{path: f, res: extraction})
	}

	result := &ASTIndexer{astRepo: s.astRepo, extractor: s.extractor}
	var resResult *ReferenceResolverResult
	if err := runASTTransaction(ctx, s.astRepo, func() error {
		if err := s.astRepo.DeleteASTNodesExceptFiles(ctx, nil); err != nil {
			return fmt.Errorf("clear ast data: %w", err)
		}
		for _, f := range extracted {
			if err := result.upsertFile(ctx, f.path, f.res); err != nil {
				return fmt.Errorf("upsert %s: %w", f.path, err)
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

	return &ASTIndexResult{
		FilesProcessed:     result.filesProcessed,
		SymbolsStored:      result.symbolsStored,
		EdgesStored:        result.edgesStored,
		ResolvedRefs:       resResult.ResolvedCount,
		AmbiguousRefs:      resResult.AmbiguousCount,
		UnresolvedNotFound: resResult.NotFoundCount,
		DurationMs:         time.Since(start).Milliseconds(),
	}, nil
}

// IndexFile re-indexes a single file: deletes its existing AST nodes, re-extracts,
// and upserts the new nodes.
func (s *ASTIndexService) IndexFile(ctx context.Context, root, relPath string) (*ASTIndexResult, error) {
	start := time.Now()

	if !isGoFile(relPath) {
		if err := runASTTransaction(ctx, s.astRepo, func() error {
			return s.astRepo.DeleteASTNodesForFile(ctx, relPath)
		}); err != nil {
			return nil, fmt.Errorf("delete old nodes for %s: %w", relPath, err)
		}
		return &ASTIndexResult{DurationMs: time.Since(start).Milliseconds()}, nil
	}

	abs := filepath.Join(root, relPath)
	source, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", abs, err)
	}

	ext, err := s.extractor.Extract(relPath, source)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", relPath, err)
	}

	result := &ASTIndexer{astRepo: s.astRepo, extractor: s.extractor}
	var resResult *ReferenceResolverResult
	if err := runASTTransaction(ctx, s.astRepo, func() error {
		if err := s.astRepo.DeleteASTNodesForFile(ctx, relPath); err != nil {
			return fmt.Errorf("delete old nodes for %s: %w", relPath, err)
		}
		if err := result.upsertFile(ctx, relPath, ext); err != nil {
			return err
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

	return &ASTIndexResult{
		FilesProcessed:     result.filesProcessed,
		SymbolsStored:      result.symbolsStored,
		EdgesStored:        result.edgesStored,
		ResolvedRefs:       resResult.ResolvedCount,
		AmbiguousRefs:      resResult.AmbiguousCount,
		UnresolvedNotFound: resResult.NotFoundCount,
		DurationMs:         time.Since(start).Milliseconds(),
	}, nil
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

func isGoFile(p string) bool {
	return strings.HasSuffix(p, ".go")
}
