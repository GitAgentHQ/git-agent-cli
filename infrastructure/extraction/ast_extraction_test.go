package extraction

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestASTExtraction(t *testing.T) {
	t.Run("extract Go function declarations", func(t *testing.T) {
		source := []byte(`package main

func HandleRequest() error { return nil }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		if result == nil {
			t.Fatal("extract returned nil result — not yet implemented")
		}

		fn := findNodeByName(result, "HandleRequest")
		if fn == nil {
			t.Fatalf("expected node named HandleRequest, got nodes: %v", nodeNames(result))
		}
		if fn.Kind != graph.ASTNodeKindFunction {
			t.Errorf("expected kind function, got %s", fn.Kind)
		}
		if fn.StartLine == 0 || fn.EndLine == 0 {
			t.Errorf("expected start_line/end_line, got %d-%d", fn.StartLine, fn.EndLine)
		}
		if fn.QualifiedName == "" {
			t.Error("expected qualified_name to be set")
		}
	})

	t.Run("extract Go method declarations with receiver", func(t *testing.T) {
		source := []byte(`package main

func (s *Server) Start() { s.run() }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		m := findNodeByName(result, "Start")
		if m == nil {
			t.Fatalf("expected node named Start, got nodes: %v", nodeNames(result))
		}
		if m.Kind != graph.ASTNodeKindMethod {
			t.Errorf("expected kind method, got %s", m.Kind)
		}
		if !strings.Contains(m.QualifiedName, "Server") {
			t.Errorf("expected qualified_name to include receiver type Server, got %s", m.QualifiedName)
		}
	})

	t.Run("extract same-named methods as distinct receiver-qualified nodes", func(t *testing.T) {
		source := []byte(`package main

type Client struct{}
type Server struct{}

func (c *Client) Connect() string { return "client" }
func (s *Server) Connect() string { return "server" }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		methods := findNodesByName(result, "Connect")
		if len(methods) != 2 {
			t.Fatalf("expected two Connect method nodes, got %d: %v", len(methods), nodeNames(result))
		}
		ids := map[string]bool{}
		qnames := map[string]bool{}
		for _, m := range methods {
			ids[m.ID] = true
			qnames[m.QualifiedName] = true
		}
		if len(ids) != 2 {
			t.Fatalf("Connect methods should have distinct IDs, got %v", ids)
		}
		if !qnames["handler.go::Client.Connect"] || !qnames["handler.go::Server.Connect"] {
			t.Fatalf("expected receiver-qualified names for both methods, got %v", qnames)
		}
	})

	t.Run("extract Go struct via type_spec", func(t *testing.T) {
		source := []byte(`package main

type Config struct { Port int }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		s := findNodeByName(result, "Config")
		if s == nil {
			t.Fatalf("expected node named Config, got nodes: %v", nodeNames(result))
		}
		if s.Kind != graph.ASTNodeKindStruct {
			t.Errorf("expected kind struct, got %s", s.Kind)
		}
	})

	t.Run("extract Go return_type for single named return", func(t *testing.T) {
		source := []byte(`package main

func NewClient() *Client { return &Client{} }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		n := findNodeByName(result, "NewClient")
		if n == nil {
			t.Fatalf("expected NewClient node, got: %v", nodeNames(result))
		}
		if n.ReturnType != "Client" {
			t.Errorf("expected ReturnType Client (unwrap pointer), got %q", n.ReturnType)
		}
	})

	t.Run("extract Go return_type takes first of multi-return", func(t *testing.T) {
		source := []byte(`package main

func Load() (*Service, error) { return nil, nil }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		n := findNodeByName(result, "Load")
		if n == nil {
			t.Fatalf("expected Load node, got: %v", nodeNames(result))
		}
		if n.ReturnType != "Service" {
			t.Errorf("expected ReturnType Service (first of multi, unwrap ptr), got %q", n.ReturnType)
		}
	})

	t.Run("extract Go return_type strips generics", func(t *testing.T) {
		source := []byte(`package main

func NewStack() *Stack[T] { return &Stack[T]{} }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		n := findNodeByName(result, "NewStack")
		if n == nil {
			t.Fatalf("expected NewStack node, got: %v", nodeNames(result))
		}
		if n.ReturnType != "Stack" {
			t.Errorf("expected ReturnType Stack (strip generics), got %q", n.ReturnType)
		}
	})

	t.Run("extract Go return_type reduces qualified name", func(t *testing.T) {
		source := []byte(`package main

import "pkg/sub"

func Build() sub.Thing { return sub.Thing{} }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		n := findNodeByName(result, "Build")
		if n == nil {
			t.Fatalf("expected Build node, got: %v", nodeNames(result))
		}
		if n.ReturnType != "Thing" {
			t.Errorf("expected ReturnType Thing (reduce qualified), got %q", n.ReturnType)
		}
	})

	t.Run("extract Go interface via type_spec", func(t *testing.T) {
		source := []byte(`package main

type Handler interface { Serve() error }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		iface := findNodeByName(result, "Handler")
		if iface == nil {
			t.Fatalf("expected node named Handler, got nodes: %v", nodeNames(result))
		}
		if iface.Kind != graph.ASTNodeKindInterface {
			t.Errorf("expected kind interface, got %s", iface.Kind)
		}
	})

	t.Run("extract call edges from function body", func(t *testing.T) {
		source := []byte(`package main

func run() { process(); log() }
func process() {}
func log() {}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		runID := findNodeIDByName(result, "run")
		processID := findNodeIDByName(result, "process")
		logID := findNodeIDByName(result, "log")

		foundProcess := false
		foundLog := false
		for _, e := range result.Edges {
			if e.Kind == graph.ASTEdgeKindCalls && e.Source == runID {
				if e.Target == processID {
					foundProcess = true
					if e.Line == 0 {
						t.Error("expected call edge to have line number")
					}
				}
				if e.Target == logID {
					foundLog = true
				}
			}
		}
		if !foundProcess {
			t.Errorf("expected call edge run->process, got edges: %v", edgeSummary(result))
		}
		if !foundLog {
			t.Errorf("expected call edge run->log, got edges: %v", edgeSummary(result))
		}
	})

	t.Run("defer var method call with ambiguous same-file methods to resolver", func(t *testing.T) {
		source := []byte(`package main

type Client struct{}
type Server struct{}

func NewClient() *Client { return &Client{} }
func (c *Client) Connect() string { return "client" }
func (s *Server) Connect() string { return "server" }

func run() string {
	svc := NewClient()
	return svc.Connect()
}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		serverConnectID := findNodeIDByQualifiedName(result, "handler.go::Server.Connect")
		runID := findNodeIDByName(result, "run")
		for _, e := range result.Edges {
			if e.Kind == graph.ASTEdgeKindCalls && e.Source == runID && e.Target == serverConnectID {
				t.Fatalf("svc.Connect should not resolve to Server.Connect by bare-name fallback: %v", edgeSummary(result))
			}
		}

		foundHintedRef := false
		for _, ref := range result.UnresolvedRefs {
			if ref.ReferenceName == "svc.Connect" && ref.VarCallHint == "NewClient" {
				foundHintedRef = true
			}
		}
		if !foundHintedRef {
			t.Fatalf("expected unresolved svc.Connect with NewClient hint, got refs: %v", refNames(result))
		}
	})

	t.Run("extract instantiates edge for direct struct construction", func(t *testing.T) {
		source := []byte(`package main

type Service struct{}

func run() {
	s := new(Service)
	_ = s
}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		// new(Service) in Go's tree-sitter grammar parses `new` as the call
		// target (a builtin) and `Service` as an argument — so no edge is
		// emitted to the Service struct. This test documents that current
		// behavior and guards against spurious edges. A future enhancement
		// could special-case the `new` builtin to emit an instantiates edge.
		for _, e := range result.Edges {
			if e.Kind == graph.ASTEdgeKindInstantiates {
				t.Errorf("unexpected instantiates edge from new(): %s -> %s", e.Source, e.Target)
			}
		}
	})

	t.Run("extract contains edges from file to symbols", func(t *testing.T) {
		source := []byte(`package main

func Foo() {}
func Bar() {}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		fileID := findNodeIDByKind(result, graph.ASTNodeKindFile)
		fooID := findNodeIDByName(result, "Foo")
		barID := findNodeIDByName(result, "Bar")

		foundFoo := false
		foundBar := false
		for _, e := range result.Edges {
			if e.Kind == graph.ASTEdgeKindContains && e.Source == fileID {
				if e.Target == fooID {
					foundFoo = true
				}
				if e.Target == barID {
					foundBar = true
				}
			}
		}
		if !foundFoo || !foundBar {
			t.Errorf("expected contains edges file->Foo and file->Bar, got %v", edgeSummary(result))
		}
	})

	t.Run("extract import declarations", func(t *testing.T) {
		source := []byte(`package main

import "fmt"
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		imp := findNodeByName(result, "fmt")
		if imp == nil {
			t.Fatalf("expected import node named fmt, got nodes: %v", nodeNames(result))
		}
		if imp.Kind != graph.ASTNodeKindImport {
			t.Errorf("expected kind import, got %s", imp.Kind)
		}

		fileID := findNodeIDByKind(result, graph.ASTNodeKindFile)
		found := false
		for _, e := range result.Edges {
			if e.Kind == graph.ASTEdgeKindImports && e.Source == fileID {
				found = true
			}
		}
		if !found {
			t.Errorf("expected imports edge from file to fmt, got %v", edgeSummary(result))
		}
	})

	t.Run("extract Go exported status", func(t *testing.T) {
		source := []byte(`package main

func PublicFunc() {}
func privateFunc() {}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		pub := findNodeByName(result, "PublicFunc")
		priv := findNodeByName(result, "privateFunc")
		if pub == nil || priv == nil {
			t.Fatalf("expected both nodes, got %v", nodeNames(result))
		}
		if !pub.IsExported {
			t.Error("expected PublicFunc to be exported")
		}
		if priv.IsExported {
			t.Error("expected privateFunc to NOT be exported")
		}
	})

	t.Run("extract function signatures", func(t *testing.T) {
		source := []byte(`package main

func Add(a int, b int) int { return a + b }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		fn := findNodeByName(result, "Add")
		if fn == nil {
			t.Fatalf("expected node named Add")
		}
		if fn.Signature == "" {
			t.Error("expected signature to be set")
		}
	})

	t.Run("unresolved refs for external calls", func(t *testing.T) {
		source := []byte(`package main

func run() { fmt.Println("hello") }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		foundPrintln := false
		for _, ref := range result.UnresolvedRefs {
			// Qualified selector calls keep the receiver/package prefix so the
			// resolver can disambiguate; both "fmt.Println" and "Println" are
			// acceptable representations of the same reference.
			if ref.ReferenceName == "Println" || strings.HasSuffix(ref.ReferenceName, ".Println") {
				foundPrintln = true
			}
		}
		if !foundPrintln {
			t.Errorf("expected unresolved ref for Println, got refs: %v", refNames(result))
		}
	})

	t.Run("empty file produces only file node", func(t *testing.T) {
		source := []byte(`package main
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("handler.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}

		symbolNodes := 0
		for _, n := range result.Nodes {
			if n.Kind != graph.ASTNodeKindFile {
				symbolNodes++
			}
		}
		if symbolNodes != 0 {
			t.Errorf("expected no symbol nodes, got %d: %v", symbolNodes, nodeNames(result))
		}
	})

	t.Run("struct embedding emits extends edge", func(t *testing.T) {
		source := []byte(`package main
type Base struct{}
type Service struct { Base }
type PtrEmbed struct { *Base }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("s.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		serviceID := findNodeIDByQualifiedName(result, "s.go::Service")
		baseID := findNodeIDByQualifiedName(result, "s.go::Base")
		ptrID := findNodeIDByQualifiedName(result, "s.go::PtrEmbed")
		if serviceID == "" || baseID == "" || ptrID == "" {
			t.Fatalf("missing expected nodes: %v", nodeNames(result))
		}
		hasExtend := func(src, dst string) bool {
			for _, e := range result.Edges {
				if e.Source == src && e.Target == dst && e.Kind == graph.ASTEdgeKindExtends {
					return true
				}
			}
			return false
		}
		if !hasExtend(serviceID, baseID) {
			t.Errorf("expected Service extends Base, edges: %v", edgeSummary(result))
		}
		if !hasExtend(ptrID, baseID) {
			t.Errorf("expected PtrEmbed extends Base (pointer unwrap), edges: %v", edgeSummary(result))
		}
	})

	t.Run("interface embedding emits implements edge", func(t *testing.T) {
		source := []byte(`package main
type Reader interface{ Read() }
type Store interface { Reader }
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("s.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		storeID := findNodeIDByQualifiedName(result, "s.go::Store")
		readerID := findNodeIDByQualifiedName(result, "s.go::Reader")
		if storeID == "" || readerID == "" {
			t.Fatalf("missing expected nodes: %v", nodeNames(result))
		}
		found := false
		for _, e := range result.Edges {
			if e.Source == storeID && e.Target == readerID && e.Kind == graph.ASTEdgeKindImplements {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Store implements Reader, edges: %v", edgeSummary(result))
		}
	})

	t.Run("named struct field is not treated as embedding", func(t *testing.T) {
		source := []byte(`package main
type Base struct{}
type Service struct {
	Base
	count int
}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("s.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		serviceID := findNodeIDByQualifiedName(result, "s.go::Service")
		// Exactly one extends edge (to Base), none to "int".
		extendsCount := 0
		for _, e := range result.Edges {
			if e.Source == serviceID && e.Kind == graph.ASTEdgeKindExtends {
				extendsCount++
			}
		}
		if extendsCount != 1 {
			t.Errorf("expected exactly 1 extends edge (Base), got %d: %v", extendsCount, edgeSummary(result))
		}
	})

	t.Run("struct fields are indexed as field nodes", func(t *testing.T) {
		source := []byte(`package main
type Command struct {
	HideHelpCommand bool
	Usage           string
}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("command.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		field := findNodeByID(result, "field:command.go:Command.HideHelpCommand")
		if field == nil {
			t.Fatalf("expected field node Command.HideHelpCommand, got nodes: %v", nodeNames(result))
		}
		if field.Kind != graph.ASTNodeKindField {
			t.Errorf("expected kind field, got %s", field.Kind)
		}
		if field.Name != "HideHelpCommand" {
			t.Errorf("expected name HideHelpCommand, got %s", field.Name)
		}
		// contains edge from the struct to the field
		structID := findNodeByID(result, "struct:command.go:Command")
		if structID == nil {
			t.Fatalf("expected struct node Command, got: %v", nodeNames(result))
		}
		foundContains := false
		for _, e := range result.Edges {
			if e.Source == structID.ID && e.Target == field.ID && e.Kind == graph.ASTEdgeKindContains {
				foundContains = true
			}
		}
		if !foundContains {
			t.Errorf("expected struct contains field edge, edges: %v", edgeSummary(result))
		}
	})

	t.Run("field reads inside a method become references edges", func(t *testing.T) {
		source := []byte(`package main
type Command struct {
	HideHelpCommand bool
}
func (c *Command) setup() {
	if c.HideHelpCommand {
		return
	}
}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("command.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		fieldID := "field:command.go:Command.HideHelpCommand"
		setupID := findNodeByID(result, "method:command.go:Command.setup")
		if setupID == nil {
			t.Fatalf("expected method Command.setup, got: %v", nodeNames(result))
		}
		foundRef := false
		for _, e := range result.Edges {
			if e.Source == setupID.ID && e.Target == fieldID && e.Kind == graph.ASTEdgeKindReferences {
				foundRef = true
			}
		}
		if !foundRef {
			t.Errorf("expected references edge setup->HideHelpCommand, edges: %v", edgeSummary(result))
		}
	})

	t.Run("receiver-var qualified call resolves to receiver type", func(t *testing.T) {
		// Two types define a same-named method; the bare "alias" key is
		// ambiguous. A receiver-var call d.alias() inside decoder.unmarshal
		// must be rewritten to decoder.alias so it resolves to the right method
		// (decoder.alias, not parser.alias). Same for p.alias() → parser.alias.
		source := []byte(`package main
type decoder struct{}
type parser struct{}

func (d *decoder) unmarshal() { d.alias() }
func (p *parser) parse()      { p.alias() }
func (d *decoder) alias()     {}
func (p *parser) alias()      {}
`)
		extractor := NewTreeSitterExtractor("go", GoExtractor())
		result, err := extractor.Extract("decode.go", source)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		// Expect calls edges to the CORRECT receiver-qualified method.
		decoderCallsTo := edgesFrom(result, "method:decode.go:decoder.unmarshal", graph.ASTEdgeKindCalls)
		parserCallsTo := edgesFrom(result, "method:decode.go:parser.parse", graph.ASTEdgeKindCalls)
		if !contains(decoderCallsTo, "method:decode.go:decoder.alias") {
			t.Errorf("expected decoder.unmarshal to call decoder.alias, got %v", decoderCallsTo)
		}
		if contains(decoderCallsTo, "method:decode.go:parser.alias") {
			t.Errorf("decoder.unmarshal must NOT call parser.alias, got %v", decoderCallsTo)
		}
		if !contains(parserCallsTo, "method:decode.go:parser.alias") {
			t.Errorf("expected parser.parse to call parser.alias, got %v", parserCallsTo)
		}
	})
}

func edgesFrom(r *graph.ExtractionResult, source string, kind graph.ASTEdgeKind) []string {
	var out []string
	for _, e := range r.Edges {
		if e.Source == source && e.Kind == kind {
			out = append(out, e.Target)
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func findNodeByName(r *graph.ExtractionResult, name string) *graph.ASTNode {
	for i := range r.Nodes {
		if r.Nodes[i].Name == name {
			return &r.Nodes[i]
		}
	}
	return nil
}

func findNodeByID(r *graph.ExtractionResult, id string) *graph.ASTNode {
	for i := range r.Nodes {
		if r.Nodes[i].ID == id {
			return &r.Nodes[i]
		}
	}
	return nil
}

func findNodesByName(r *graph.ExtractionResult, name string) []graph.ASTNode {
	var out []graph.ASTNode
	for i := range r.Nodes {
		if r.Nodes[i].Name == name {
			out = append(out, r.Nodes[i])
		}
	}
	return out
}

func findNodeIDByName(r *graph.ExtractionResult, name string) string {
	n := findNodeByName(r, name)
	if n == nil {
		return ""
	}
	return n.ID
}

func findNodeIDByQualifiedName(r *graph.ExtractionResult, qname string) string {
	for i := range r.Nodes {
		if r.Nodes[i].QualifiedName == qname {
			return r.Nodes[i].ID
		}
	}
	return ""
}

func findNodeIDByKind(r *graph.ExtractionResult, kind graph.ASTNodeKind) string {
	for i := range r.Nodes {
		if r.Nodes[i].Kind == kind {
			return r.Nodes[i].ID
		}
	}
	return ""
}

func nodeNames(r *graph.ExtractionResult) []string {
	names := make([]string, len(r.Nodes))
	for i, n := range r.Nodes {
		names[i] = string(n.Kind) + ":" + n.Name
	}
	return names
}

func edgeSummary(r *graph.ExtractionResult) []string {
	out := make([]string, len(r.Edges))
	for i, e := range r.Edges {
		out[i] = e.Source + " " + string(e.Kind) + " " + e.Target
	}
	return out
}

func refNames(r *graph.ExtractionResult) []string {
	out := make([]string, len(r.UnresolvedRefs))
	for i, ref := range r.UnresolvedRefs {
		out[i] = ref.ReferenceName
	}
	return out
}
