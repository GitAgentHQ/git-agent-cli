package extraction

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/gitagenthq/git-agent/domain/extraction"
	"github.com/gitagenthq/git-agent/domain/graph"
)

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
	parser := sitter.NewParser()
	defer parser.Close()

	lang := sitter.NewLanguage(tree_sitter_go.Language())
	parser.SetLanguage(lang)

	tree := parser.Parse(source, nil)
	if tree == nil {
		return &graph.ExtractionResult{DurationMs: time.Since(start).Milliseconds()}, fmt.Errorf("parser returned nil tree for %s", filePath)
	}
	defer tree.Close()

	root := tree.RootNode()

	state := &extractState{
		filePath:    filePath,
		source:      source,
		lang:        e.lang,
		extractor:   e.extractor,
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

	// Pass 1: extract all symbols (function/class/method/struct/interface/type/import/variable)
	state.visitSymbols(root)

	// Pass 2: extract call edges and unresolved refs from function bodies
	state.visitCalls(root)

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

func (s *extractState) visitSymbols(node *sitter.Node) {
	if node == nil || !node.IsNamed() {
		return
	}

	nodeType := node.Kind()
	skipChildren := false

	if slices.Contains(s.extractor.TypeAliasTypes, nodeType) {
		skipChildren = s.extractTypeAlias(node)
	} else if slices.Contains(s.extractor.FunctionTypes, nodeType) {
		if s.isInsideClassLikeNode() && slices.Contains(s.extractor.MethodTypes, nodeType) {
			skipChildren = s.extractMethod(node)
		} else {
			skipChildren = s.extractFunction(node)
		}
	} else if slices.Contains(s.extractor.MethodTypes, nodeType) {
		skipChildren = s.extractMethod(node)
	} else if slices.Contains(s.extractor.InterfaceTypes, nodeType) {
		skipChildren = s.extractInterface(node)
	} else if slices.Contains(s.extractor.StructTypes, nodeType) {
		skipChildren = s.extractStruct(node)
	} else if slices.Contains(s.extractor.ImportTypes, nodeType) {
		s.extractImport(node)
	} else if slices.Contains(s.extractor.VariableTypes, nodeType) && !s.isInsideFunctionNode() {
		s.extractVariable(node)
		skipChildren = true
	}

	if !skipChildren {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			s.visitSymbols(child)
		}
	}
}

func (s *extractState) visitCalls(node *sitter.Node) {
	if node == nil || !node.IsNamed() {
		return
	}

	nodeType := node.Kind()

	if slices.Contains(s.extractor.CallTypes, nodeType) {
		s.extractCall(node)
	}
	// Track variable → type from `name := NewX()` so subsequent `name.Method()`
	// calls can be disambiguated to `Type.Method`.
	s.recordVarType(node)

	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		s.visitCalls(child)
	}
}

// recordVarType inspects a short_var_declaration / var_spec / assignment_statement
// whose RHS is a call (e.g. `svc := NewClient()` or `svc = NewClient()`) and
// records varName → the called symbol name (NOT the resolved return type —
// that join happens in the ReferenceResolver, which has all ASTNodes across
// files). This is a best-effort receiver-type inference for chained method
// calls (codegraph #645).
func (s *extractState) recordVarType(node *sitter.Node) {
	switch node.Kind() {
	case "short_var_declaration", "var_spec":
		s.recordVarTypeFromDecl(node)
	case "assignment_statement":
		s.recordVarTypeFromAssign(node)
	}
}

func (s *extractState) recordVarTypeFromDecl(node *sitter.Node) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil {
		// Fallback: Go short_var_declaration has unnamed children (left id, := , right expr)
		var names []string
		var rhs *sitter.Node
		for i := uint(0); i < node.NamedChildCount(); i++ {
			c := node.NamedChild(i)
			if c.Kind() == "identifier" && rhs == nil {
				names = append(names, s.nodeText(c))
			} else {
				rhs = c
			}
		}
		if rhs == nil || len(names) == 0 {
			return
		}
		calledName := s.calledSymbolName(rhs)
		if calledName == "" {
			return
		}
		for _, vn := range names {
			s.varCalls[vn] = calledName
		}
		return
	}
	varNames := s.extractAssignmentNames(left)
	calledName := s.calledSymbolName(right)
	if calledName == "" {
		return
	}
	for _, vn := range varNames {
		s.varCalls[vn] = calledName
	}
}

func (s *extractState) recordVarTypeFromAssign(node *sitter.Node) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil {
		return
	}
	varNames := s.extractAssignmentNames(left)
	calledName := s.calledSymbolName(right)
	if calledName == "" {
		return
	}
	for _, vn := range varNames {
		s.varCalls[vn] = calledName
	}
}

// extractAssignmentNames collects identifier names from the LHS of an
// assignment (a, b := ... or x = ...). The Go grammar wraps a single LHS in an
// expression_list, so we unwrap it.
func (s *extractState) extractAssignmentNames(left *sitter.Node) []string {
	var names []string
	if left == nil {
		return names
	}
	if left.Kind() == "expression_list" {
		for i := uint(0); i < left.NamedChildCount(); i++ {
			c := left.NamedChild(i)
			if c.Kind() == "identifier" {
				names = append(names, s.nodeText(c))
			}
		}
		return names
	}
	if left.Kind() == "identifier" {
		names = append(names, s.nodeText(left))
		return names
	}
	for i := uint(0); i < left.NamedChildCount(); i++ {
		c := left.NamedChild(i)
		if c.Kind() == "identifier" {
			names = append(names, s.nodeText(c))
		}
	}
	return names
}

// calledSymbolName returns the called symbol name (trailing part for
// qualified calls like pkg.NewClient) when node is a call_expression, e.g.
// NewClient() → "NewClient". The Go grammar wraps a single RHS in an
// expression_list, so we unwrap it. Returns "" otherwise.
func (s *extractState) calledSymbolName(node *sitter.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "expression_list" && node.NamedChildCount() == 1 {
		node = node.NamedChild(0)
	}
	if node.Kind() != "call_expression" {
		return ""
	}
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return ""
	}
	calledName := s.resolveCallName(funcNode)
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

func (s *extractState) extractFunction(node *sitter.Node) bool {
	name := s.extractName(node)
	if name == "" {
		return false
	}

	astNode := s.createSymbolNode(graph.ASTNodeKindFunction, name, node)
	astNode.IsExported = s.isGoExported(name)
	astNode.Signature = s.extractGoSignature(node)
	astNode.ReturnType = s.extractGoReturnType(node)
	s.registerSymbol(name, astNode)

	s.nodes = append(s.nodes, astNode)
	s.emitContainsEdge(astNode.ID)
	symbolID := astNode.ID
	s.nodeStack = append(s.nodeStack, symbolID)

	bodyNode := node.ChildByFieldName(s.extractor.BodyField)
	if bodyNode != nil {
		s.visitFunctionBody(bodyNode, symbolID)
	}

	s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
	return true
}

func (s *extractState) extractMethod(node *sitter.Node) bool {
	name := s.extractName(node)
	if name == "" {
		return false
	}

	astNode := s.createSymbolNode(graph.ASTNodeKindMethod, name, node)
	astNode.IsExported = s.isGoExported(name)
	astNode.Signature = s.extractGoSignature(node)
	astNode.ReturnType = s.extractGoReturnType(node)

	receiver := s.extractGoReceiver(node)
	if receiver != "" {
		astNode.ID = string(graph.ASTNodeKindMethod) + ":" + s.filePath + ":" + receiver + "." + name
		astNode.QualifiedName = s.filePath + "::" + receiver + "." + name
		s.registerSymbol(receiver+"."+name, astNode)
		s.registerSymbol(name, astNode)
		if s.extractor.MethodsAreTopLevel {
			s.emitContainsEdge(astNode.ID)
			parentID := s.nodeStack[len(s.nodeStack)-1]
			s.edges = append(s.edges, graph.ASTEdge{
				Source:     parentID,
				Target:     astNode.ID,
				Kind:       graph.ASTEdgeKindContains,
				Provenance: "tree-sitter",
			})
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

	bodyNode := node.ChildByFieldName(s.extractor.BodyField)
	if bodyNode != nil {
		s.visitFunctionBody(bodyNode, symbolID)
	}

	s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
	return true
}

func (s *extractState) extractInterface(node *sitter.Node) bool {
	name := s.extractName(node)
	if name == "" {
		return false
	}

	astNode := s.createSymbolNode(graph.ASTNodeKindInterface, name, node)
	astNode.IsExported = s.isGoExported(name)
	s.registerSymbol(name, astNode)
	s.nodes = append(s.nodes, astNode)
	s.emitContainsEdge(astNode.ID)

	s.nodeStack = append(s.nodeStack, astNode.ID)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		s.visitSymbols(child)
	}
	s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
	return true
}

func (s *extractState) extractStruct(node *sitter.Node) bool {
	name := s.extractName(node)
	if name == "" {
		return false
	}

	astNode := s.createSymbolNode(graph.ASTNodeKindStruct, name, node)
	astNode.IsExported = s.isGoExported(name)
	s.registerSymbol(name, astNode)
	s.nodes = append(s.nodes, astNode)
	s.emitContainsEdge(astNode.ID)

	s.nodeStack = append(s.nodeStack, astNode.ID)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		s.visitSymbols(child)
	}
	s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
	return true
}

func (s *extractState) extractTypeAlias(node *sitter.Node) bool {
	name := s.extractName(node)
	if name == "" {
		return false
	}

	resolvedKind := s.resolveGoTypeAliasKind(node)
	kind := graph.ASTNodeKindTypeAlias
	if resolvedKind != "" {
		kind = graph.ASTNodeKind(resolvedKind)
	}

	astNode := s.createSymbolNode(kind, name, node)
	astNode.IsExported = s.isGoExported(name)
	s.registerSymbol(name, astNode)
	s.nodes = append(s.nodes, astNode)
	s.emitContainsEdge(astNode.ID)

	if kind == graph.ASTNodeKindStruct || kind == graph.ASTNodeKindInterface {
		s.nodeStack = append(s.nodeStack, astNode.ID)
		typeChild := node.ChildByFieldName("type")
		if typeChild != nil {
			// Extract embedded types as extends/implements edges. Go embeds a
			// type by naming it as a field with no field_identifier (struct) or
			// as a bare type_elem (interface). The promoted methods of the
			// embedded type become available on this type, so an edge to it
			// lets impact analysis and the resolver reach them.
			s.extractEmbeddings(typeChild, astNode.ID, kind)
			for i := uint(0); i < typeChild.NamedChildCount(); i++ {
				child := typeChild.NamedChild(i)
				s.visitSymbols(child)
			}
		}
		s.nodeStack = s.nodeStack[:len(s.nodeStack)-1]
		return true
	}
	return true
}

// extractEmbeddings walks a struct_type or interface_type body and emits an
// extends (struct embedding) or implements (interface embedding) edge for each
// embedded type. An embedded field is a field_declaration with no
// field_identifier child (just a type); an embedded interface element is a
// type_elem whose child is a bare type identifier. Embedded types may be
// cross-file or package-qualified, so unresolved ones become ASTUnresolvedRef
// entries with ReferenceKind "extends"/"implements" for the resolver to link.
func (s *extractState) extractEmbeddings(typeNode *sitter.Node, containerID string, containerKind graph.ASTNodeKind) {
	edgeKind := graph.ASTEdgeKindExtends
	if containerKind == graph.ASTNodeKindInterface {
		edgeKind = graph.ASTEdgeKindImplements
	}

	walkTypeBody(typeNode, func(fieldNode *sitter.Node) {
		typeNode := embeddedTypeNode(fieldNode, containerKind)
		if typeNode == nil {
			return
		}
		typeName := s.nodeText(typeNode)
		if typeName == "" {
			return
		}
		// Resolve only unqualified, same-file embedded types now; qualified
		// or cross-file embeddings are left for the resolver.
		if id, ok := s.symbolIDs[typeName]; ok {
			s.edges = append(s.edges, graph.ASTEdge{
				Source:     containerID,
				Target:     id,
				Kind:       edgeKind,
				Line:       int(typeNode.StartPosition().Row) + 1,
				Provenance: "tree-sitter",
			})
			return
		}
		s.unresolved = append(s.unresolved, graph.ASTUnresolvedRef{
			FromNodeID:    containerID,
			ReferenceName: typeName,
			ReferenceKind: string(edgeKind),
			Line:          int(typeNode.StartPosition().Row) + 1,
			Column:        int(typeNode.StartPosition().Column),
			FilePath:      s.filePath,
			Language:      s.lang,
		})
	})
}

// walkTypeBody invokes fn for each embedded-field/element node under a
// struct_type or interface_type. struct_type nests a field_declaration_list
// of field_declaration nodes; interface_type nests type_elem nodes. The
// field_declaration_list is the first named child that is not a comment.
func walkTypeBody(typeNode *sitter.Node, fn func(*sitter.Node)) {
	switch typeNode.Kind() {
	case "struct_type":
		for i := uint(0); i < typeNode.NamedChildCount(); i++ {
			c := typeNode.NamedChild(i)
			if c.Kind() == "field_declaration_list" {
				for j := uint(0); j < c.NamedChildCount(); j++ {
					fn(c.NamedChild(j))
				}
				return
			}
		}
	case "interface_type":
		for i := uint(0); i < typeNode.NamedChildCount(); i++ {
			fn(typeNode.NamedChild(i))
		}
	}
}

// embeddedTypeNode returns the type node of an embedded field, or nil if the
// field is a regular named field (not an embedding).
func embeddedTypeNode(fieldNode *sitter.Node, containerKind graph.ASTNodeKind) *sitter.Node {
	if containerKind == graph.ASTNodeKindInterface {
		// type_elem → bare type_identifier (embedded interface) or a method
		// signature (func). Only bare type identifiers are embeddings.
		if fieldNode.Kind() == "type_elem" {
			for i := uint(0); i < fieldNode.NamedChildCount(); i++ {
				c := fieldNode.NamedChild(i)
				if c.Kind() == "type_identifier" {
					return c
				}
			}
		}
		return nil
	}
	// struct field_declaration: an embedding has a type_identifier (or
	// qualified_type) child but NO field_identifier child.
	if fieldNode.Kind() != "field_declaration" {
		return nil
	}
	hasFieldName := false
	var typeChild *sitter.Node
	for i := uint(0); i < fieldNode.NamedChildCount(); i++ {
		c := fieldNode.NamedChild(i)
		switch c.Kind() {
		case "field_identifier":
			hasFieldName = true
		case "type_identifier", "qualified_type", "pointer_type", "slice_type", "generic_type":
			typeChild = c
		}
	}
	if hasFieldName || typeChild == nil {
		return nil
	}
	return unwrapEmbeddedType(typeChild)
}

// unwrapEmbeddedType follows pointer/slice/generic wrappers to the underlying
// type name node (e.g. *Base → Base, pkg.Base → Base).
func unwrapEmbeddedType(n *sitter.Node) *sitter.Node {
	for n != nil {
		switch n.Kind() {
		case "pointer_type", "slice_type":
			if n.NamedChildCount() > 0 {
				n = n.NamedChild(0)
				continue
			}
			return nil
		case "qualified_type":
			// pkg.Type → take the type_identifier child
			if n.NamedChildCount() > 0 {
				last := n.NamedChild(n.NamedChildCount() - 1)
				if last.Kind() == "type_identifier" {
					return last
				}
			}
			return n
		case "generic_type":
			if n.NamedChildCount() > 0 {
				n = n.NamedChild(0)
				continue
			}
			return nil
		default:
			return n
		}
	}
	return n
}

func (s *extractState) extractImport(node *sitter.Node) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		switch child.Kind() {
		case "import_spec":
			s.extractSingleImportSpec(child)
		case "import_spec_list":
			for j := uint(0); j < child.NamedChildCount(); j++ {
				spec := child.NamedChild(j)
				if spec.Kind() == "import_spec" {
					s.extractSingleImportSpec(spec)
				}
			}
		}
	}
}

func (s *extractState) extractSingleImportSpec(node *sitter.Node) {
	pathNode := node.ChildByFieldName("path")
	if pathNode == nil {
		return
	}

	importPath := s.nodeText(pathNode)
	importPath = strings.Trim(importPath, "\"")

	astNode := graph.ASTNode{
		ID:            "import:" + s.filePath + ":" + importPath,
		Kind:          graph.ASTNodeKindImport,
		Name:          importPath,
		QualifiedName: importPath,
		FilePath:      s.filePath,
		Language:      s.lang,
		StartLine:     int(node.StartPosition().Row) + 1,
		EndLine:       int(node.EndPosition().Row) + 1,
		StartColumn:   int(node.StartPosition().Column),
		EndColumn:     int(node.EndPosition().Column),
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

func (s *extractState) extractCall(node *sitter.Node) {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return
	}

	callName := s.resolveCallName(funcNode)
	if callName == "" {
		return
	}

	// Find the enclosing function/method node by walking up the AST
	fromID := s.findEnclosingSymbolID(node)
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
			Line:       int(node.StartPosition().Row) + 1,
			Provenance: "tree-sitter",
		})
	} else {
		ref := graph.ASTUnresolvedRef{
			FromNodeID:    fromID,
			ReferenceName: callName,
			ReferenceKind: string(graph.ASTEdgeKindCalls),
			Line:          int(node.StartPosition().Row) + 1,
			Column:        int(node.StartPosition().Column),
			FilePath:      s.filePath,
			Language:      s.lang,
			VarCallHint:   varCallHint,
		}
		s.unresolved = append(s.unresolved, ref)
	}
}

func (s *extractState) findEnclosingSymbolID(node *sitter.Node) string {
	current := node.Parent()
	for current != nil {
		key := s.declarationKey(current)
		if key != "" {
			if id, ok := s.symbolIDs[key]; ok {
				return id
			}
		}
		current = current.Parent()
	}
	// Fallback to file node
	if len(s.nodes) > 0 && s.nodes[0].Kind == graph.ASTNodeKindFile {
		return s.nodes[0].ID
	}
	return ""
}

func (s *extractState) extractVariable(node *sitter.Node) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "var_spec" || child.Kind() == "const_spec" {
			s.extractVarSpec(child)
		}
	}
}

func (s *extractState) extractVarSpec(node *sitter.Node) {
	nameNode := node.ChildByFieldName(s.extractor.NameField)
	if nameNode == nil {
		return
	}

	// var_spec/const_spec can have multiple names
	names := s.extractNameList(node)
	kind := graph.ASTNodeKindVariable
	if node.Kind() == "const_spec" {
		kind = graph.ASTNodeKindConstant
	}

	for _, name := range names {
		astNode := s.createSymbolNodeAtPosition(kind, name, int(node.StartPosition().Row)+1, int(node.EndPosition().Row)+1, int(node.StartPosition().Column), int(node.EndPosition().Column))
		astNode.IsExported = s.isGoExported(name)
		s.registerSymbol(name, astNode)
		s.nodes = append(s.nodes, astNode)
		s.emitContainsEdge(astNode.ID)
	}
}

func (s *extractState) extractNameList(node *sitter.Node) []string {
	var names []string
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "identifier" {
			names = append(names, s.nodeText(child))
		}
	}
	return names
}

func (s *extractState) visitFunctionBody(body *sitter.Node, fromID string) {
	for i := uint(0); i < body.NamedChildCount(); i++ {
		child := body.NamedChild(i)
		s.visitSymbols(child)
	}
}

func (s *extractState) extractName(node *sitter.Node) string {
	nameNode := node.ChildByFieldName(s.extractor.NameField)
	if nameNode != nil {
		return s.nodeText(nameNode)
	}
	return ""
}

func (s *extractState) createSymbolNode(kind graph.ASTNodeKind, name string, node *sitter.Node) graph.ASTNode {
	qname := s.filePath + "::" + name
	id := string(kind) + ":" + s.filePath + ":" + name

	astNode := graph.ASTNode{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: qname,
		FilePath:      s.filePath,
		Language:      s.lang,
		StartLine:     int(node.StartPosition().Row) + 1,
		EndLine:       int(node.EndPosition().Row) + 1,
		StartColumn:   int(node.StartPosition().Column),
		EndColumn:     int(node.EndPosition().Column),
		UpdatedAt:     time.Now().UnixMilli(),
	}

	return astNode
}

func (s *extractState) createSymbolNodeAtPosition(kind graph.ASTNodeKind, name string, startLine, endLine, startCol, endCol int) graph.ASTNode {
	qname := s.filePath + "::" + name
	id := string(kind) + ":" + s.filePath + ":" + name

	astNode := graph.ASTNode{
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

	return astNode
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

func (s *extractState) declarationKey(node *sitter.Node) string {
	if node == nil {
		return ""
	}
	name := s.extractName(node)
	if name == "" {
		return ""
	}
	if slices.Contains(s.extractor.MethodTypes, node.Kind()) {
		receiver := s.extractGoReceiver(node)
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

func (s *extractState) nodeText(node *sitter.Node) string {
	start := node.StartByte()
	end := node.EndByte()
	if int(end) > len(s.source) {
		end = uint(len(s.source))
	}
	if int(start) >= len(s.source) {
		return ""
	}
	return string(s.source[start:end])
}

func (s *extractState) resolveCallName(funcNode *sitter.Node) string {
	switch funcNode.Kind() {
	case "identifier":
		return s.nodeText(funcNode)
	case "selector_expression":
		// Keep the qualifier (receiver/package) so the resolver can disambiguate
		// methods with the same name across types (e.g. svc.Commit vs tx.Commit).
		// The resolver matches on the trailing method part plus this hint.
		field := funcNode.ChildByFieldName("field")
		if field == nil {
			return ""
		}
		operand := funcNode.ChildByFieldName("operand")
		method := s.nodeText(field)
		if operand == nil {
			return method
		}
		return s.resolveCallName(operand) + "." + method
	case "parenthesized_expression":
		inner := funcNode.NamedChild(0)
		if inner != nil {
			return s.resolveCallName(inner)
		}
		return ""
	default:
		return s.nodeText(funcNode)
	}
}

func (s *extractState) isGoExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	first := name[0]
	return first >= 'A' && first <= 'Z'
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

func (s *extractState) extractGoSignature(node *sitter.Node) string {
	paramsNode := node.ChildByFieldName("parameters")
	resultNode := node.ChildByFieldName("result")

	sig := ""
	if paramsNode != nil {
		sig = s.nodeText(paramsNode)
	}
	if resultNode != nil {
		sig += " " + s.nodeText(resultNode)
	}
	return sig
}

// extractGoReturnType infers the primary return type of a Go function/method
// declaration for receiver inference in chained calls (e.g. NewClient().Send()
// resolves Send against Client). Rules (mirroring codegraph #645/#608):
//   - unwrap leading pointer: *Foo → Foo
//   - multi-return (Foo, error): take the first
//   - strip generics: Foo[T] → Foo
//   - reduce qualified name: pkg.Foo → Foo
func (s *extractState) extractGoReturnType(node *sitter.Node) string {
	resultNode := node.ChildByFieldName("result")
	if resultNode == nil {
		return ""
	}
	raw := strings.TrimSpace(s.nodeText(resultNode))
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
		// "x int" → "int" (drop a leading identifier that isn't a type).
		if parts := strings.Fields(raw); len(parts) >= 2 {
			raw = parts[len(parts)-1]
		}
	}

	raw = strings.TrimPrefix(raw, "*")

	// Strip generics: Foo[T] → Foo.
	if idx := strings.Index(raw, "["); idx >= 0 {
		raw = raw[:idx]
	}

	// Reduce qualified name: pkg.Foo → Foo.
	if idx := strings.LastIndex(raw, "."); idx >= 0 {
		raw = raw[idx+1:]
	}

	return strings.TrimSpace(raw)
}

func (s *extractState) extractGoReceiver(node *sitter.Node) string {
	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}
	text := s.nodeText(receiver)
	text = strings.Trim(text, "()")
	parts := strings.SplitN(text, " ", 2)
	if len(parts) == 2 {
		typeName := strings.TrimPrefix(parts[1], "*")
		return typeName
	}
	return strings.TrimPrefix(text, "*")
}

func (s *extractState) resolveGoTypeAliasKind(node *sitter.Node) string {
	typeChild := node.ChildByFieldName("type")
	if typeChild == nil {
		return ""
	}
	switch typeChild.Kind() {
	case "struct_type":
		return "struct"
	case "interface_type":
		return "interface"
	default:
		return ""
	}
}

func (s *extractState) isInsideClassLikeNode() bool {
	for i := len(s.nodeStack) - 1; i >= 0; i-- {
		id := s.nodeStack[i]
		if strings.HasPrefix(id, "struct:") || strings.HasPrefix(id, "interface:") || strings.HasPrefix(id, "class:") {
			return true
		}
	}
	return false
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
