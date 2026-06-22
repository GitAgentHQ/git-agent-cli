package application

import (
	"context"
	"fmt"
	"io"
	"strings"

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

	idxResult, err := NewASTIndexService(s.astRepo, s.git, s.extractor).IndexAll(ctx, root)
	if err != nil {
		return fmt.Errorf("index AST symbols: %w", err)
	}
	if progress != nil && idxResult.FilesProcessed > 0 {
		fmt.Fprintf(progress, "AST indexed %d files, %d symbols [%dms]\n",
			idxResult.FilesProcessed, idxResult.SymbolsStored, idxResult.DurationMs)
	}
	return s.stateRepo.SetIndexState(ctx, astIndexHeadKey, head)
}

func (s *ASTEnsureIndexService) hasGoWorkingTreeChanges(ctx context.Context) (bool, error) {
	changed, err := s.git.DiffNameOnly(ctx)
	if err != nil {
		return false, fmt.Errorf("list working-tree changes for AST freshness: %w", err)
	}
	for _, f := range changed {
		if strings.HasSuffix(f, ".go") && !graph.IsToolingPath(f) {
			return true, nil
		}
	}
	return false, nil
}
