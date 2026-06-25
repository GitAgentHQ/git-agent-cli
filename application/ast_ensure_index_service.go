package application

import (
	"context"
	"fmt"
	"io"

	"github.com/gitagenthq/git-agent/domain/extraction"
	"github.com/gitagenthq/git-agent/domain/graph"
)

const astIndexHeadKey = "ast_index_head"

type ASTIndexStateRepository interface {
	GetIndexState(ctx context.Context, key string) (string, error)
	SetIndexState(ctx context.Context, key, value string) error
}

type ASTIndexGitClient interface {
	TrackedFileLister
	CurrentHead(ctx context.Context) (string, error)
	DiffNameOnly(ctx context.Context) ([]string, error)
	DiffNameOnlySince(ctx context.Context, sinceHash string) ([]string, error)
	MergeBaseIsAncestor(ctx context.Context, ancestor, head string) (bool, error)
}

type ASTEnsureIndexService struct {
	astRepo   graph.ASTRepository
	stateRepo ASTIndexStateRepository
	git       ASTIndexGitClient
	extractor extraction.SymbolExtractor
}

func NewASTEnsureIndexService(astRepo graph.ASTRepository, stateRepo ASTIndexStateRepository, git ASTIndexGitClient, extractor extraction.SymbolExtractor) *ASTEnsureIndexService {
	return &ASTEnsureIndexService{
		astRepo:   astRepo,
		stateRepo: stateRepo,
		git:       git,
		extractor: extractor,
	}
}

// EnsureAll brings the AST index up to date for queries that are not scoped to
// a single symbol (search, node-by-name). It mirrors EnsureForSymbol's freshness
// check but always resolves to a full or incremental index without a symbol
// lookup, so an empty index or a stale head triggers IndexAll / IndexFiles.
func (s *ASTEnsureIndexService) EnsureAll(ctx context.Context, root string, force bool, progress io.Writer) error {
	head, err := s.git.CurrentHead(ctx)
	if err != nil {
		return fmt.Errorf("current head for AST index: %w", err)
	}
	indexedHead, err := s.stateRepo.GetIndexState(ctx, astIndexHeadKey)
	if err != nil {
		return err
	}
	hasGoChanges, err := s.hasGoWorkingTreeChanges(ctx)
	if err != nil {
		return err
	}
	if !force && indexedHead == head && !hasGoChanges {
		return nil
	}

	idxSvc := NewASTIndexService(s.astRepo, s.git, s.extractor)
	var idxResult *ASTIndexResult
	switch {
	case force || indexedHead == "":
		idxResult, err = idxSvc.IndexAll(ctx, root)
	default:
		reachable, reachErr := s.git.MergeBaseIsAncestor(ctx, indexedHead, head)
		if reachErr != nil {
			return fmt.Errorf("check AST index reachability: %w", reachErr)
		}
		if !reachable {
			idxResult, err = idxSvc.IndexAll(ctx, root)
			break
		}
		files, collectErr := s.collectIncrementalGoFiles(ctx, indexedHead, hasGoChanges)
		if collectErr != nil {
			return collectErr
		}
		pruneStale := indexedHead != head
		if len(files) == 0 {
			if pruneStale {
				idxResult, err = idxSvc.IndexFiles(ctx, root, nil, true)
			}
			break
		}
		idxResult, err = idxSvc.IndexFiles(ctx, root, files, pruneStale)
	}
	if err != nil {
		return fmt.Errorf("index AST symbols: %w", err)
	}
	if progress != nil && idxResult != nil && idxResult.FilesProcessed > 0 {
		fmt.Fprintf(progress, "AST indexed %d files, %d symbols [%dms]\n",
			idxResult.FilesProcessed, idxResult.SymbolsStored, idxResult.DurationMs)
	}
	return s.stateRepo.SetIndexState(ctx, astIndexHeadKey, head)
}

func (s *ASTEnsureIndexService) EnsureForSymbol(ctx context.Context, root, symbol string, force bool, progress io.Writer) error {
	head, err := s.git.CurrentHead(ctx)
	if err != nil {
		return fmt.Errorf("current head for AST index: %w", err)
	}
	indexedHead, err := s.stateRepo.GetIndexState(ctx, astIndexHeadKey)
	if err != nil {
		return err
	}
	nodes, err := s.astRepo.GetASTNodeByName(ctx, symbol)
	if err != nil {
		return fmt.Errorf("lookup AST symbol %q: %w", symbol, err)
	}
	hasGoChanges, err := s.hasGoWorkingTreeChanges(ctx)
	if err != nil {
		return err
	}
	if !force && indexedHead == head && !hasGoChanges && len(nodes) > 0 {
		return nil
	}

	idxSvc := NewASTIndexService(s.astRepo, s.git, s.extractor)
	var idxResult *ASTIndexResult

	switch {
	case force || indexedHead == "":
		idxResult, err = idxSvc.IndexAll(ctx, root)
	default:
		reachable, reachErr := s.git.MergeBaseIsAncestor(ctx, indexedHead, head)
		if reachErr != nil {
			return fmt.Errorf("check AST index reachability: %w", reachErr)
		}
		if !reachable {
			idxResult, err = idxSvc.IndexAll(ctx, root)
			break
		}

		files, collectErr := s.collectIncrementalGoFiles(ctx, indexedHead, hasGoChanges)
		if collectErr != nil {
			return collectErr
		}
		pruneStale := indexedHead != head
		if len(files) == 0 {
			if len(nodes) == 0 {
				idxResult, err = idxSvc.IndexAll(ctx, root)
				break
			}
			if pruneStale {
				idxResult, err = idxSvc.IndexFiles(ctx, root, nil, true)
			}
			break
		}
		idxResult, err = idxSvc.IndexFiles(ctx, root, files, pruneStale)
	}

	if err != nil {
		return fmt.Errorf("index AST symbols: %w", err)
	}

	if len(nodes) == 0 {
		nodes, err = s.astRepo.GetASTNodeByName(ctx, symbol)
		if err != nil {
			return fmt.Errorf("re-check AST symbol %q: %w", symbol, err)
		}
		if len(nodes) == 0 {
			idxResult, err = idxSvc.IndexAll(ctx, root)
			if err != nil {
				return fmt.Errorf("full AST re-index for missing symbol: %w", err)
			}
		}
	}

	if progress != nil && idxResult != nil && idxResult.FilesProcessed > 0 {
		fmt.Fprintf(progress, "AST indexed %d files, %d symbols [%dms]\n",
			idxResult.FilesProcessed, idxResult.SymbolsStored, idxResult.DurationMs)
	}
	return s.stateRepo.SetIndexState(ctx, astIndexHeadKey, head)
}

func (s *ASTEnsureIndexService) collectIncrementalGoFiles(ctx context.Context, indexedHead string, hasGoChanges bool) ([]string, error) {
	var files []string
	if hasGoChanges {
		changed, err := s.git.DiffNameOnly(ctx)
		if err != nil {
			return nil, fmt.Errorf("list working-tree changes for AST index: %w", err)
		}
		files = mergeUniqueStrings(files, filterGoFiles(changed))
	}
	if indexedHead != "" {
		committed, err := s.git.DiffNameOnlySince(ctx, indexedHead)
		if err != nil {
			return nil, fmt.Errorf("list commits since AST index head: %w", err)
		}
		files = mergeUniqueStrings(files, filterGoFiles(committed))
	}
	return files, nil
}

func (s *ASTEnsureIndexService) hasGoWorkingTreeChanges(ctx context.Context) (bool, error) {
	changed, err := s.git.DiffNameOnly(ctx)
	if err != nil {
		return false, fmt.Errorf("list working-tree changes for AST freshness: %w", err)
	}
	for _, f := range changed {
		if isGoFile(f) && !graph.IsToolingPath(f) {
			return true, nil
		}
	}
	return false, nil
}
