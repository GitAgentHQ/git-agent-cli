package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

func TestFormatASTImpactText_CallersShownWithFileAndKind(t *testing.T) {
	result := &graph.ASTImpactResult{
		SeedNode: graph.ASTNode{Name: "runHandler", Kind: graph.ASTNodeKindFunction},
		Impacted: []graph.ASTImpactEntry{
			{Node: graph.ASTNode{Name: "main", Kind: graph.ASTNodeKindFunction, FilePath: "cmd/main.go"}, Edge: graph.ASTEdge{Kind: graph.ASTEdgeKindCalls}, Depth: 1},
			{Node: graph.ASTNode{Name: "serveHTTP", Kind: graph.ASTNodeKindMethod, FilePath: "server.go"}, Edge: graph.ASTEdge{Kind: graph.ASTEdgeKindCalls}, Depth: 1},
		},
		TotalFound: 2,
	}

	var buf bytes.Buffer
	outputASTImpactText(&buf, result)

	out := buf.String()
	if !strings.Contains(out, "runHandler") {
		t.Errorf("output should show seed symbol name: %q", out)
	}
	if !strings.Contains(out, "main") || !strings.Contains(out, "serveHTTP") {
		t.Errorf("output should show impacted symbols: %q", out)
	}
	if !strings.Contains(out, "server.go") {
		t.Errorf("output should show file paths: %q", out)
	}
	if !strings.Contains(out, "function") || !strings.Contains(out, "method") {
		t.Errorf("output should show symbol kinds: %q", out)
	}
}

func TestFormatASTImpactText_DepthMarkerShown(t *testing.T) {
	result := &graph.ASTImpactResult{
		SeedNode: graph.ASTNode{Name: "processData", Kind: graph.ASTNodeKindFunction},
		Impacted: []graph.ASTImpactEntry{
			{Node: graph.ASTNode{Name: "direct", Kind: graph.ASTNodeKindFunction, FilePath: "a.go"}, Depth: 1},
			{Node: graph.ASTNode{Name: "indirect", Kind: graph.ASTNodeKindFunction, FilePath: "b.go"}, Depth: 2},
		},
		TotalFound: 2,
	}

	var buf bytes.Buffer
	outputASTImpactText(&buf, result)

	out := buf.String()
	if !strings.Contains(out, "d2") {
		t.Errorf("depth-2 entry should show depth marker: %q", out)
	}
}

func TestFormatASTImpactText_EmptyShowsMessage(t *testing.T) {
	result := &graph.ASTImpactResult{
		SeedNode: graph.ASTNode{Name: "lonely", Kind: graph.ASTNodeKindFunction},
	}

	var buf bytes.Buffer
	outputASTImpactText(&buf, result)

	out := buf.String()
	if !strings.Contains(out, "lonely") {
		t.Errorf("output should still show seed name: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "no callers") && !strings.Contains(strings.ToLower(out), "no impacted") {
		t.Errorf("empty result should show a no-results message: %q", out)
	}
}

func TestFormatASTImpactJSON_ValidJSON(t *testing.T) {
	result := &graph.ASTImpactResult{
		SeedNode:   graph.ASTNode{Name: "foo", Kind: graph.ASTNodeKindFunction},
		Impacted:   []graph.ASTImpactEntry{},
		TotalFound: 0,
		QueryMs:    5,
	}

	var buf bytes.Buffer
	if err := outputASTImpactJSON(&buf, result); err != nil {
		t.Fatalf("outputASTImpactJSON: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "seed_node") {
		t.Errorf("JSON output should include seed node: %q", out)
	}
	if !strings.Contains(out, "impacted") {
		t.Errorf("JSON output should include impacted array: %q", out)
	}
}
