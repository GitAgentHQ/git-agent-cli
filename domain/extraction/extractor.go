package extraction

import "github.com/gitagenthq/git-agent/domain/graph"

type SymbolExtractor interface {
	Language() string
	Extract(filePath string, source []byte) (*graph.ExtractionResult, error)
}
