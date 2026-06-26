package extraction

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"time"

	"github.com/gitagenthq/git-agent/domain/extraction"
	"github.com/gitagenthq/git-agent/domain/graph"
)

// TreeSitterExtractor extracts AST symbols, edges, and unresolved references
// from Go source using the standard library go/parser + go/ast. It produces
// output byte-for-byte compatible with the prior tree-sitter implementation
// (same node IDs, qualified names, edge kinds, and provenance tag), so the
// ASTRepository and ReferenceResolver are unchanged.
//
// The name is retained for call-site stability (cmd/impact.go and tests
// reference NewTreeSitterExtractor/GoExtractor verbatim).
type TreeSitterExtractor struct {
	lang      string
	extractor *LanguageExtractor
}

func NewTreeSitterExtractor(language string, extractor *LanguageExtractor) *TreeSitterExtractor {
	return &TreeSitterExtractor{
		lang:      language,
		extractor: extractor,
	}
}

func (e *TreeSitterExtractor) Language() string {
	return e.lang
}

var _ extraction.SymbolExtractor = (*TreeSitterExtractor)(nil)

func (e *TreeSitterExtractor) Extract(filePath string, source []byte) (*graph.ExtractionResult, error) {
	start := time.Now()

	if e.lang != "go" {
		return &graph.ExtractionResult{DurationMs: time.Since(start).Milliseconds()}, fmt.Errorf("unsupported language %q", e.lang)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, source, parser.ParseComments)
	if err != nil {
		return &graph.ExtractionResult{DurationMs: time.Since(start).Milliseconds()}, fmt.Errorf("parse %s: %w", filePath, err)
	}

	state := &extractState{
		filePath:    filePath,
		source:      source,
		lang:        e.lang,
		extractor:   e.extractor,
		fset:        fset,
		nodes:       []graph.ASTNode{},
		edges:       []graph.ASTEdge{},
		unresolved:  []graph.ASTUnresolvedRef{},
		nodeStack:   []string{},
		symbolIDs:   map[string]string{},
		symbolKinds: map[string]graph.ASTNodeKind{},
		ambiguous:   map[string]bool{},
		varCalls:    map[string]string{},
	}

	fileNode := state.createFileNode(filePath, source)
	state.nodeStack = append(state.nodeStack, fileNode.ID)

	// Pass 1: extract all symbols (function/method/struct/interface/type_alias/import/variable/constant)
	state.visitSymbols(f)

	// Pass 2: extract call edges and unresolved refs from function bodies
	state.visitCalls(f)

	state.nodeStack = state.nodeStack[:len(state.nodeStack)-1]

	return &graph.ExtractionResult{
		Nodes:          state.nodes,
		Edges:          state.edges,
		UnresolvedRefs: state.unresolved,
		DurationMs:     time.Since(start).Milliseconds(),
	}, nil
}

type extractState struct {
	filePath    string
	source      []byte
	lang        string
	extractor   *LanguageExtractor
	fset        *token.FileSet
	nodes       []graph.ASTNode
	edges       []graph.ASTEdge
	unresolved  []graph.ASTUnresolvedRef
	nodeStack   []string
	symbolIDs   map[string]string
	symbolKinds map[string]graph.ASTNodeKind
	ambiguous   map[string]bool
	// varCalls maps a local variable name to the called symbol name from a
	// `name := NewX()` assignment (the return-type join is deferred to the
	// ReferenceResolver). Used to disambiguate `var.Method()` calls.
	varCalls map[string]string
}

func (s *extractState) createFileNode(filePath string, source []byte) graph.ASTNode {
	lineCount := bytes.Count(source, []byte{'\n'}) + 1
	now := time.Now().UnixMilli()
	node := graph.ASTNode{
		ID:            "file:" + filePath,
		Kind:          graph.ASTNodeKindFile,
		Name:          filepath.Base(filePath),
		QualifiedName: filePath,
		FilePath:      filePath,
		Language:      s.lang,
		StartLine:     1,
		EndLine:       lineCount,
		StartColumn:   0,
		EndColumn:     0,
		UpdatedAt:     now,
	}
	s.nodes = append(s.nodes, node)
	return node
}

// visitSymbols is Pass 1: iterate top-level declarations. Go has no nested
// declarations at file scope, so iterating Decls covers all symbols; function
// bodies are walked explicitly for nested container types (none expected in Go,
// but the recursion is kept for parity with the prior implementation).
func (s *extractState) visitSymbols(f *ast.File) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				s.extractFunction(d)
			} else {
				s.extractMethod(d)
			}
		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						s.extractTypeAlias(ts)
					}
				}
			case token.IMPORT:
				s.extractImport(d)
			case token.VAR, token.CONST:
				if !s.isInsideFunctionNode() {
					s.extractVariable(d)
				}
			}
		}
	}
}

// visitCalls is Pass 2: a full-tree DFS that emits call edges / unresolved
// refs and records varCalls from assignments. A manual symbol stack tracks
// the enclosing function/method so findEnclosingSymbolID can attribute calls.
// Because ast.Inspect's nil-callback does not identify which node is exiting,
// the FuncDecl ID is pushed before inspecting its body and popped after —
// scoping each function's calls to its own subtree.
func (s *extractState) visitCalls(f *ast.File) {
	var funcDepth []string
	inspect := func(n ast.Node) {
		ast.Inspect(n, func(node ast.Node) bool {
			if ce, ok := node.(*ast.CallExpr); ok {
				s.extractCall(ce, funcDepth)
			}
			s.recordVarType(node)
			return true
		})
	}

	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			// Non-function top-level decls (imports/types/vars) may still hold
			// call expressions in initializers; attribute them to the file.
			inspect(decl)
			continue
		}
		id := s.declarationKey(fd)
		sid := ""
		if id != "" {
			if v, ok := s.symbolIDs[id]; ok {
				sid = v
			}
		}
		funcDepth = append(funcDepth, sid)
		if fd.Body != nil {
			inspect(fd.Body)
		}
		funcDepth = funcDepth[:len(funcDepth)-1]
	}
}

// recordVarType inspects an assignment (:= or =) whose RHS is a call and
// records varName -> called symbol name (NOT the resolved return type). This
// is a best-effort receiver-type inference for chained method calls.
func (s *extractState) recordVarType(n ast.Node) {
	as, ok := n.(*ast.AssignStmt)
	if !ok {
		return
	}
	if len(as.Lhs) != len(as.Rhs) {
		return
	}
	if len(as.Lhs) >= 2 {
		// Multi-value: pair each LHS identifier with its positionally
		// corresponding RHS call.
		for i := range as.Lhs {
			lc, ok := as.Lhs[i].(*ast.Ident)
			if !ok {
				continue
			}
			calledName := s.calledSymbolName(as.Rhs[i])
			if calledName == "" {
				continue
			}
			s.varCalls[lc.Name] = calledName
		}
		return
	}
	// Single value: map every LHS identifier to the single RHS call.
	calledName := s.calledSymbolName(as.Rhs[0])
	if calledName == "" {
		return
	}
	for _, lhs := range as.Lhs {
		if lc, ok := lhs.(*ast.Ident); ok {
			s.varCalls[lc.Name] = calledName
		}
	}
}

// calledSymbolName returns the called symbol name (trailing part for
// qualified calls like pkg.NewClient) when node is a call expression, e.g.
// NewClient() -> "NewClient". Returns "" otherwise.
func (s *extractState) calledSymbolName(expr ast.Expr) string {
	ce, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	calledName := s.resolveCallName(ce.Fun)
	if calledName == "" {
		return ""
	}
	// For qualified calls (pkg.NewClient), use the trailing part — the
	// qualifier is resolved by the resolver via imports/packages.
	if dot := strings.LastIndex(calledName, "."); dot >= 0 {
		calledName = calledName[dot+1:]
	}
	return calledName
}

func (s *extractState) extractFunction(d *ast.FuncDecl) bool {
	name := d.Name.Name
	if name == "" {
		return false
	}

	astNode := s.createSymbolNode(graph.ASTNodeKindFunction, name, d)
	astNode.IsExported = token.IsExported(name)
	astNode.Signature = s.extractGoSignature(d)
	astNode.ReturnType = s.extractGoReturnType(d)
	s.registerSymbol(name, astNode)

	s.nodes = append(s.nodes, astNode)
	s.emitContainsEdge(astNode.ID)
	symbolID := astNode.ID
	s.nodeStack = append(s.nodeStack, symbolID)

	if d.Body != nil {
		s.visitFunctionBody(d.Body)
	}

	s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
	return true
}

func (s *extractState) extractMethod(d *ast.FuncDecl) bool {
	name := d.Name.Name
	if name == "" {
		return false
	}

	astNode := s.createSymbolNode(graph.ASTNodeKindMethod, name, d)
	astNode.IsExported = token.IsExported(name)
	astNode.Signature = s.extractGoSignature(d)
	astNode.ReturnType = s.extractGoReturnType(d)

	receiver := s.extractGoReceiver(d)
	if receiver != "" {
		astNode.ID = string(graph.ASTNodeKindMethod) + ":" + s.filePath + ":" + receiver + "." + name
		astNode.QualifiedName = s.filePath + "::" + receiver + "." + name
		s.registerSymbol(receiver+"."+name, astNode)
		s.registerSymbol(name, astNode)
		if s.extractor.MethodsAreTopLevel {
			s.emitContainsEdge(astNode.ID)
		}
	} else {
		s.registerSymbol(name, astNode)
	}

	s.nodes = append(s.nodes, astNode)
	if !s.extractor.MethodsAreTopLevel || receiver == "" {
		s.emitContainsEdge(astNode.ID)
	}

	symbolID := astNode.ID
	s.nodeStack = append(s.nodeStack, symbolID)

	if d.Body != nil {
		s.visitFunctionBody(d.Body)
	}

	s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
	return true
}

func (s *extractState) extractTypeAlias(ts *ast.TypeSpec) bool {
	name := ts.Name.Name
	if name == "" {
		return false
	}

	resolvedKind := s.resolveGoTypeAliasKind(ts)
	kind := graph.ASTNodeKindTypeAlias
	if resolvedKind != "" {
		kind = graph.ASTNodeKind(resolvedKind)
	}

	astNode := s.createSymbolNode(kind, name, ts)
	astNode.IsExported = token.IsExported(name)
	s.registerSymbol(name, astNode)
	s.nodes = append(s.nodes, astNode)
	s.emitContainsEdge(astNode.ID)

	if kind == graph.ASTNodeKindStruct || kind == graph.ASTNodeKindInterface {
		s.nodeStack = append(s.nodeStack, astNode.ID)
		// Extract embedded types as extends/implements edges. The promoted
		// methods of the embedded type become available on this type, so an
		// edge to it lets impact analysis and the resolver reach them.
		s.extractEmbeddings(ts.Type, astNode.ID, kind)
		switch t := ts.Type.(type) {
		case *ast.StructType:
			if t.Fields != nil {
				for _, field := range t.Fields.List {
					s.visitFieldDecl(field)
				}
			}
		case *ast.InterfaceType:
			if t.Methods != nil {
				for _, field := range t.Methods.List {
					s.visitFieldDecl(field)
				}
			}
		}
		s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
		return true
	}
	return true
}

// visitFieldDecl walks a struct/interface field for nested symbols. Go has no
// nested declarations here, so this is kept for parity and is effectively a
// no-op for symbol extraction; embedded types are handled in extractEmbeddings.
func (s *extractState) visitFieldDecl(_ *ast.Field) {
	// no nested symbols to extract in Go
}

// extractEmbeddings walks a struct or interface body and emits an extends
// (struct embedding) or implements (interface embedding) edge for each embedded
// type. Embedded types may be cross-file or package-qualified, so unresolved
// ones become ASTUnresolvedRef entries with ReferenceKind "extends"/"implements".
//
// To preserve the prior tree-sitter behavior, only bare (unqualified) embedded
// type identifiers are considered for interface embeddings; qualified embedded
// interfaces are left unresolved to the resolver (tree-sitter dropped them, and
// no test covers the difference — the gap is preserved intentionally).
func (s *extractState) extractEmbeddings(typeExpr ast.Expr, containerID string, containerKind graph.ASTNodeKind) {
	edgeKind := graph.ASTEdgeKindExtends
	if containerKind == graph.ASTNodeKindInterface {
		edgeKind = graph.ASTEdgeKindImplements
	}

	switch t := typeExpr.(type) {
	case *ast.StructType:
		if t.Fields == nil {
			return
		}
		for _, field := range t.Fields.List {
			if len(field.Names) > 0 {
				continue // named field, not an embedding
			}
			typeName := s.embeddedTypeName(field.Type, containerKind)
			if typeName == "" {
				continue
			}
			s.emitEmbeddingEdge(field, containerID, typeName, edgeKind)
		}
	case *ast.InterfaceType:
		if t.Methods == nil {
			return
		}
		for _, field := range t.Methods.List {
			if len(field.Names) > 0 {
				continue // method signature, not an embedding
			}
			if _, isFunc := field.Type.(*ast.FuncType); isFunc {
				continue
			}
			typeName := s.embeddedTypeName(field.Type, containerKind)
			if typeName == "" {
				continue
			}
			s.emitEmbeddingEdge(field, containerID, typeName, edgeKind)
		}
	}
}

func (s *extractState) emitEmbeddingEdge(field *ast.Field, containerID, typeName string, edgeKind graph.ASTEdgeKind) {
	pos := s.fset.Position(field.Pos())
	if id, ok := s.symbolIDs[typeName]; ok {
		s.edges = append(s.edges, graph.ASTEdge{
			Source:     containerID,
			Target:     id,
			Kind:       edgeKind,
			Line:       pos.Line,
			Provenance: "tree-sitter",
		})
		return
	}
	s.unresolved = append(s.unresolved, graph.ASTUnresolvedRef{
		FromNodeID:    containerID,
		ReferenceName: typeName,
		ReferenceKind: string(edgeKind),
		Line:          pos.Line,
		Column:        pos.Column - 1,
		FilePath:      s.filePath,
		Language:      s.lang,
	})
}

// embeddedTypeName returns the name of an embedded type after unwrapping
// pointer/slice/qualified/generic wrappers, or "" if not a recognized
// embeddable type. For interface embeddings, only bare type identifiers are
// recognized (preserving the prior tree-sitter gap).
func (s *extractState) embeddedTypeName(typeExpr ast.Expr, containerKind graph.ASTNodeKind) string {
	if containerKind == graph.ASTNodeKindInterface {
		// Bare type_identifier only; qualified embedded interfaces were
		// dropped by the prior extractor — preserve that gap.
		if id, ok := typeExpr.(*ast.Ident); ok {
			return id.Name
		}
		return ""
	}
	n := unwrapEmbeddedType(typeExpr)
	if n == nil {
		return ""
	}
	if id, ok := n.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// unwrapEmbeddedType follows pointer/slice/generic wrappers to the underlying
// type identifier (e.g. *Base -> Base, pkg.Base -> Base).
func unwrapEmbeddedType(expr ast.Expr) ast.Expr {
	for expr != nil {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
			continue
		case *ast.ArrayType:
			expr = t.Elt
			continue
		case *ast.IndexExpr:
			expr = t.X
			continue
		case *ast.IndexListExpr:
			expr = t.X
			continue
		case *ast.SelectorExpr:
			// pkg.Type -> take the type_identifier child (the selector).
			return t.Sel
		default:
			return expr
		}
	}
	return expr
}

func (s *extractState) extractImport(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		imp, ok := spec.(*ast.ImportSpec)
		if !ok {
			continue
		}
		s.extractSingleImportSpec(imp)
	}
}

func (s *extractState) extractSingleImportSpec(spec *ast.ImportSpec) {
	if spec.Path == nil {
		return
	}

	importPath := strings.Trim(spec.Path.Value, "\"")

	startPos := s.fset.Position(spec.Pos())
	endPos := s.fset.Position(spec.End())
	astNode := graph.ASTNode{
		ID:            "import:" + s.filePath + ":" + importPath,
		Kind:          graph.ASTNodeKindImport,
		Name:          importPath,
		QualifiedName: importPath,
		FilePath:      s.filePath,
		Language:      s.lang,
		StartLine:     startPos.Line,
		EndLine:       endPos.Line,
		StartColumn:   startPos.Column - 1,
		EndColumn:     endPos.Column - 1,
		UpdatedAt:     time.Now().UnixMilli(),
	}
	s.nodes = append(s.nodes, astNode)

	parentID := s.nodeStack[len(s.nodeStack)-1]
	s.edges = append(s.edges, graph.ASTEdge{
		Source:     parentID,
		Target:     astNode.ID,
		Kind:       graph.ASTEdgeKindImports,
		Provenance: "tree-sitter",
	})
}

func (s *extractState) extractCall(ce *ast.CallExpr, funcDepth []string) {
	callName := s.resolveCallName(ce.Fun)
	if callName == "" {
		return
	}

	fromID := s.findEnclosingSymbolID(funcDepth)
	if fromID == "" {
		return
	}

	// Same-file resolution: try the exact qualified name first. For selector
	// calls with a factory hint (svc := NewX(); svc.Method()), defer to the
	// resolver so the receiver type can disambiguate same-named methods.
	targetID := s.symbolIDs[callName]
	targetKind := graph.ASTNodeKind("")
	varCallHint := ""
	if dot := strings.LastIndex(callName, "."); dot >= 0 {
		if calledName, ok := s.varCalls[callName[:dot]]; ok {
			varCallHint = calledName
		}
	}
	if targetID == "" {
		if dot := strings.LastIndex(callName, "."); dot >= 0 && varCallHint == "" {
			bare := callName[dot+1:]
			targetID = s.symbolIDs[bare]
			targetKind = s.symbolKinds[bare]
		}
	} else {
		targetKind = s.symbolKinds[callName]
	}
	pos := s.fset.Position(ce.Pos())
	if targetID != "" {
		// A call whose target is a struct/class is a construction: classify it
		// as instantiates so callers/callees stay symmetric (issue #774).
		edgeKind := graph.ASTEdgeKindCalls
		if isInstantiableKind(targetKind) {
			edgeKind = graph.ASTEdgeKindInstantiates
		}
		s.edges = append(s.edges, graph.ASTEdge{
			Source:     fromID,
			Target:     targetID,
			Kind:       edgeKind,
			Line:       pos.Line,
			Provenance: "tree-sitter",
		})
	} else {
		ref := graph.ASTUnresolvedRef{
			FromNodeID:    fromID,
			ReferenceName: callName,
			ReferenceKind: string(graph.ASTEdgeKindCalls),
			Line:          pos.Line,
			Column:        pos.Column - 1,
			FilePath:      s.filePath,
			Language:      s.lang,
			VarCallHint:   varCallHint,
		}
		s.unresolved = append(s.unresolved, ref)
	}
}

func (s *extractState) findEnclosingSymbolID(funcDepth []string) string {
	for i := len(funcDepth) - 1; i >= 0; i-- {
		if funcDepth[i] != "" {
			return funcDepth[i]
		}
	}
	// Fallback to file node
	if len(s.nodes) > 0 && s.nodes[0].Kind == graph.ASTNodeKindFile {
		return s.nodes[0].ID
	}
	return ""
}

func (s *extractState) extractVariable(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		s.extractVarSpec(vs, d.Tok)
	}
}

func (s *extractState) extractVarSpec(vs *ast.ValueSpec, tok token.Token) {
	if len(vs.Names) == 0 {
		return
	}

	kind := graph.ASTNodeKindVariable
	if tok == token.CONST {
		kind = graph.ASTNodeKindConstant
	}

	startPos := s.fset.Position(vs.Pos())
	endPos := s.fset.Position(vs.End())
	for _, nameNode := range vs.Names {
		name := nameNode.Name
		astNode := s.createSymbolNodeAtPosition(kind, name, startPos.Line, endPos.Line, startPos.Column-1, endPos.Column-1)
		astNode.IsExported = token.IsExported(name)
		s.registerSymbol(name, astNode)
		s.nodes = append(s.nodes, astNode)
		s.emitContainsEdge(astNode.ID)
	}
}

func (s *extractState) visitFunctionBody(body *ast.BlockStmt) {
	// Go has no nested declarations at function scope reachable as top-level
	// symbols; kept for parity with the prior implementation.
}

func (s *extractState) createSymbolNode(kind graph.ASTNodeKind, name string, node ast.Node) graph.ASTNode {
	qname := s.filePath + "::" + name
	id := string(kind) + ":" + s.filePath + ":" + name

	startPos := s.fset.Position(node.Pos())
	endPos := s.fset.Position(node.End())
	return graph.ASTNode{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: qname,
		FilePath:      s.filePath,
		Language:      s.lang,
		StartLine:     startPos.Line,
		EndLine:       endPos.Line,
		StartColumn:   startPos.Column - 1,
		EndColumn:     endPos.Column - 1,
		UpdatedAt:     time.Now().UnixMilli(),
	}
}

func (s *extractState) createSymbolNodeAtPosition(kind graph.ASTNodeKind, name string, startLine, endLine, startCol, endCol int) graph.ASTNode {
	qname := s.filePath + "::" + name
	id := string(kind) + ":" + s.filePath + ":" + name

	return graph.ASTNode{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: qname,
		FilePath:      s.filePath,
		Language:      s.lang,
		StartLine:     startLine,
		EndLine:       endLine,
		StartColumn:   startCol,
		EndColumn:     endCol,
		UpdatedAt:     time.Now().UnixMilli(),
	}
}

func (s *extractState) registerSymbol(key string, astNode graph.ASTNode) {
	if key == "" {
		return
	}
	if existing, ok := s.symbolIDs[key]; ok && existing != astNode.ID {
		delete(s.symbolIDs, key)
		delete(s.symbolKinds, key)
		s.ambiguous[key] = true
		return
	}
	if s.ambiguous[key] {
		return
	}
	s.symbolIDs[key] = astNode.ID
	s.symbolKinds[key] = astNode.Kind
}

// declarationKey returns the registry key for a function/method declaration:
// bare name for functions, Receiver.Name for methods.
func (s *extractState) declarationKey(fd *ast.FuncDecl) string {
	name := fd.Name.Name
	if name == "" {
		return ""
	}
	if fd.Recv != nil {
		receiver := s.extractGoReceiver(fd)
		if receiver != "" {
			return receiver + "." + name
		}
	}
	return name
}

func (s *extractState) emitContainsEdge(childID string) {
	if len(s.nodeStack) == 0 {
		return
	}
	parentID := s.nodeStack[len(s.nodeStack)-1]
	s.edges = append(s.edges, graph.ASTEdge{
		Source:     parentID,
		Target:     childID,
		Kind:       graph.ASTEdgeKindContains,
		Provenance: "tree-sitter",
	})
}

// resolveCallName returns the called symbol name, keeping the qualifier for
// selector expressions (receiver/package) so the resolver can disambiguate
// methods with the same name across types (e.g. svc.Commit vs tx.Commit).
func (s *extractState) resolveCallName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		method := e.Sel.Name
		if e.X == nil {
			return method
		}
		return s.resolveCallName(e.X) + "." + method
	case *ast.ParenExpr:
		if e.X != nil {
			return s.resolveCallName(e.X)
		}
		return ""
	default:
		return ""
	}
}

// isInstantiableKind reports whether a node kind represents a constructable
// type, so a call targeting it should be classified as instantiates.
func isInstantiableKind(kind graph.ASTNodeKind) bool {
	switch kind {
	case graph.ASTNodeKindClass, graph.ASTNodeKindStruct, graph.ASTNodeKindInterface:
		return true
	}
	return false
}

func (s *extractState) extractGoSignature(d *ast.FuncDecl) string {
	sig := ""
	if d.Type.Params != nil {
		sig = s.nodeText(d.Type.Params)
	}
	if d.Type.Results != nil {
		sig += " " + s.nodeText(d.Type.Results)
	}
	return sig
}

// extractGoReturnType infers the primary return type of a Go function/method
// declaration for receiver inference in chained calls (e.g. NewClient().Send()
// resolves Send against Client). Rules (mirroring codegraph #645/#608):
//   - unwrap leading pointer: *Foo -> Foo
//   - multi-return (Foo, error): take the first
//   - strip generics: Foo[T] -> Foo
//   - reduce qualified name: pkg.Foo -> Foo
func (s *extractState) extractGoReturnType(d *ast.FuncDecl) string {
	if d.Type.Results == nil {
		return ""
	}
	raw := strings.TrimSpace(s.nodeText(d.Type.Results))
	if raw == "" {
		return ""
	}

	// Single vs multi return. A bare type has no comma; a parameter list like
	// "(Foo, error)" or "(x int, err error)" starts with "(".
	if strings.HasPrefix(raw, "(") {
		raw = strings.Trim(raw, "()")
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return ""
		}
		// Take the type portion before the first comma. Named returns like
		// "x int, err error" still yield "int" — good enough for inference.
		if idx := strings.Index(raw, ","); idx >= 0 {
			raw = strings.TrimSpace(raw[:idx])
		}
		// "x int" -> "int" (drop a leading identifier that isn't a type).
		if parts := strings.Fields(raw); len(parts) >= 2 {
			raw = parts[len(parts)-1]
		}
	}

	raw = strings.TrimPrefix(raw, "*")

	// Strip generics: Foo[T] -> Foo.
	if idx := strings.Index(raw, "["); idx >= 0 {
		raw = raw[:idx]
	}

	// Reduce qualified name: pkg.Foo -> Foo.
	if idx := strings.LastIndex(raw, "."); idx >= 0 {
		raw = raw[idx+1:]
	}

	return strings.TrimSpace(raw)
}

// nodeText returns the source slice spanned by a node, mirroring the prior
// tree-sitter nodeText behavior (raw source bytes, no reformatting).
func (s *extractState) nodeText(n ast.Node) string {
	start := s.fset.Position(n.Pos()).Offset
	end := s.fset.Position(n.End()).Offset
	if end > len(s.source) {
		end = len(s.source)
	}
	if start >= len(s.source) || start > end {
		return ""
	}
	return string(s.source[start:end])
}

func (s *extractState) extractGoReceiver(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	t := d.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func (s *extractState) resolveGoTypeAliasKind(ts *ast.TypeSpec) string {
	switch ts.Type.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	default:
		return ""
	}
}

func (s *extractState) isInsideFunctionNode() bool {
	for i := len(s.nodeStack) - 1; i >= 0; i-- {
		id := s.nodeStack[i]
		if strings.HasPrefix(id, "function:") || strings.HasPrefix(id, "method:") {
			return true
		}
	}
	return false
}
