//go:build !cgo

package extraction

import (
	"errors"

	"github.com/gitagenthq/git-agent/domain/extraction"
	"github.com/gitagenthq/git-agent/domain/graph"
)

// errASTRequiresCgo is returned by the cgo-free build's stub extractor. The
// release binaries are built with CGO_ENABLED=0; AST extraction depends on
// tree-sitter, which is cgo-only. Build from source with CGO_ENABLED=1 to use
// the AST-dependent graph commands (impact, callers, callees, index, sync,
// node, query, affected).
var errASTRequiresCgo = errors.New("AST extraction is unavailable in this build (compiled without cgo); build with CGO_ENABLED=1 to enable graph impact/callers/callees/index commands")

// TreeSitterExtractor is a no-op stub when compiled without cgo.
type TreeSitterExtractor struct{}

func NewTreeSitterExtractor(language string, extractor *LanguageExtractor) *TreeSitterExtractor {
	return &TreeSitterExtractor{}
}

func (e *TreeSitterExtractor) Language() string { return "go" }

func (e *TreeSitterExtractor) Extract(filePath string, source []byte) (*graph.ExtractionResult, error) {
	return &graph.ExtractionResult{}, errASTRequiresCgo
}

var _ extraction.SymbolExtractor = (*TreeSitterExtractor)(nil)
