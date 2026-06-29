package graph

// CommitNode represents a single git commit in the graph.
type CommitNode struct {
	Hash         string
	Message      string
	AuthorName   string
	AuthorEmail  string
	Timestamp    int64
	ParentHashes []string
}

// FileNode represents a tracked file.
type FileNode struct {
	Path            string
	Language        string
	LastIndexedHash string
}

// AuthorNode represents a commit author.
type AuthorNode struct {
	Email string
	Name  string
}

// ModifiesEdge links a commit to a file it modified.
type ModifiesEdge struct {
	CommitHash string
	FilePath   string
	Additions  int
	Deletions  int
	Status     string // A (added), M (modified), D (deleted), R (renamed)
}

// CoChangedEntry represents a co-change relationship between two files.
type CoChangedEntry struct {
	FileA            string
	FileB            string
	CouplingCount    int
	CouplingStrength float64
	LastCoupledHash  string
}

// RenameEntry tracks a file rename across commits.
type RenameEntry struct {
	OldPath    string
	NewPath    string
	CommitHash string
}

// CommitInfo is the structured output of a detailed git log entry,
// used during indexing.
type CommitInfo struct {
	Hash         string
	Message      string
	AuthorName   string
	AuthorEmail  string
	Timestamp    int64
	ParentHashes []string
	Files        []FileChange
}

// FileChange describes a single file modification within a commit.
type FileChange struct {
	Path      string
	OldPath   string // populated for renames
	Status    string // A, M, D, R
	Additions int
	Deletions int
}

// =============================================================================
// AST (Abstract Syntax Tree) types — code structure layer
// =============================================================================

// ASTNodeKind enumerates the kinds of code symbols extracted from AST parsing.
type ASTNodeKind string

const (
	ASTNodeKindFile       ASTNodeKind = "file"
	ASTNodeKindModule     ASTNodeKind = "module"
	ASTNodeKindClass      ASTNodeKind = "class"
	ASTNodeKindStruct     ASTNodeKind = "struct"
	ASTNodeKindInterface  ASTNodeKind = "interface"
	ASTNodeKindTrait      ASTNodeKind = "trait"
	ASTNodeKindFunction   ASTNodeKind = "function"
	ASTNodeKindMethod     ASTNodeKind = "method"
	ASTNodeKindProperty   ASTNodeKind = "property"
	ASTNodeKindField      ASTNodeKind = "field"
	ASTNodeKindVariable   ASTNodeKind = "variable"
	ASTNodeKindConstant   ASTNodeKind = "constant"
	ASTNodeKindEnum       ASTNodeKind = "enum"
	ASTNodeKindEnumMember ASTNodeKind = "enum_member"
	ASTNodeKindTypeAlias  ASTNodeKind = "type_alias"
	ASTNodeKindNamespace  ASTNodeKind = "namespace"
	ASTNodeKindParameter  ASTNodeKind = "parameter"
	ASTNodeKindImport     ASTNodeKind = "import"
	ASTNodeKindExport     ASTNodeKind = "export"
	ASTNodeKindRoute      ASTNodeKind = "route"
	ASTNodeKindComponent  ASTNodeKind = "component"
)

// ASTEdgeKind enumerates the kinds of relationships between AST nodes.
type ASTEdgeKind string

const (
	ASTEdgeKindContains     ASTEdgeKind = "contains"
	ASTEdgeKindCalls        ASTEdgeKind = "calls"
	ASTEdgeKindImports      ASTEdgeKind = "imports"
	ASTEdgeKindExports      ASTEdgeKind = "exports"
	ASTEdgeKindExtends      ASTEdgeKind = "extends"
	ASTEdgeKindImplements   ASTEdgeKind = "implements"
	ASTEdgeKindReferences   ASTEdgeKind = "references"
	ASTEdgeKindTypeOf       ASTEdgeKind = "type_of"
	ASTEdgeKindReturns      ASTEdgeKind = "returns"
	ASTEdgeKindInstantiates ASTEdgeKind = "instantiates"
	ASTEdgeKindOverrides    ASTEdgeKind = "overrides"
	ASTEdgeKindDecorates    ASTEdgeKind = "decorates"
)

// ASTNode represents a code symbol extracted from a source file's AST.
type ASTNode struct {
	ID            string      `json:"id"`
	Kind          ASTNodeKind `json:"kind"`
	Name          string      `json:"name"`
	QualifiedName string      `json:"qualified_name"`
	FilePath      string      `json:"file_path"`
	Language      string      `json:"language"`
	StartLine     int         `json:"start_line"`
	EndLine       int         `json:"end_line"`
	StartColumn   int         `json:"start_column"`
	EndColumn     int         `json:"end_column"`
	Signature     string      `json:"signature"`
	Visibility    string      `json:"visibility"` // public, private, protected, internal
	IsExported    bool        `json:"is_exported"`
	IsAsync       bool        `json:"is_async"`
	IsStatic      bool        `json:"is_static"`
	IsAbstract    bool        `json:"is_abstract"`
	ReturnType    string      `json:"return_type"`
	UpdatedAt     int64       `json:"updated_at"`
}

// ASTEdge represents a relationship between two AST nodes.
type ASTEdge struct {
	Source     string      `json:"source"`
	Target     string      `json:"target"`
	Kind       ASTEdgeKind `json:"kind"`
	Line       int         `json:"line"`
	Column     int         `json:"column"`
	Provenance string      `json:"provenance"`         // tree-sitter, heuristic, resolver
	Metadata   string      `json:"metadata,omitempty"` // JSON blob: resolvedBy, confidence, etc.
}

// ASTUnresolvedRef is a reference that could not be resolved during extraction.
type ASTUnresolvedRef struct {
	FromNodeID    string
	ReferenceName string
	ReferenceKind string // ASTEdgeKind or "function_ref"
	Line          int
	Column        int
	FilePath      string
	Language      string
	// VarCallHint carries the called symbol name when this ref's qualifier is a
	// local variable assigned from a factory call, e.g. for `svc.Method()`
	// where `svc := NewClient()`, VarCallHint = "NewClient". The ReferenceResolver
	// looks up NewClient's ReturnType to disambiguate the method receiver.
	VarCallHint string
}

// ExtractionResult holds the output of parsing a single source file.
type ExtractionResult struct {
	Nodes          []ASTNode
	Edges          []ASTEdge
	UnresolvedRefs []ASTUnresolvedRef
	DurationMs     int64
}

// ASTImpactResult holds the output of a structural impact query.
type ASTImpactResult struct {
	SeedNode   ASTNode          `json:"seed_node"`
	Impacted   []ASTImpactEntry `json:"impacted"`
	TotalFound int              `json:"total_found"`
	QueryMs    int64            `json:"query_ms"`
}

// ASTImpactEntry is a single node in a structural impact result.
type ASTImpactEntry struct {
	Node  ASTNode `json:"node"`
	Edge  ASTEdge `json:"edge"`
	Depth int     `json:"depth"`
}

// ASTSearchResult is a node found via symbol search.
type ASTSearchResult struct {
	Node  ASTNode `json:"node"`
	Score float64 `json:"score"`
}
