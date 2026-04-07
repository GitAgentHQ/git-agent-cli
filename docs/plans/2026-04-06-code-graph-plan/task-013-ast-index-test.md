# Task 013: AST Indexing Test

**depends-on**: task-005, task-012

## Description

Write tests for integrating AST parsing into the indexing flow: creating Symbol nodes, CONTAINS edges, CALLS edges, IMPORTS edges during indexing, and rebuilding symbols when file content changes.

## Execution Context

**Task Number**: 013 of 020 (test)
**Phase**: P1 - AST Indexing
**Prerequisites**: Full index (task-005) and Tree-sitter parser (task-012)

## BDD Scenario

```gherkin
Scenario: Index extracts CALLS relationships from AST
  Given the repository contains a Go file "pkg/service.go" with content:
      """
      package pkg
      func Process(input string) string {
          result := Transform(input)
          return Format(result)
      }
      func Transform(s string) string { return s }
      func Format(s string) string { return s }
      """
  When I run "git-agent graph index"
  Then the graph should contain a CALLS edge from "Process" to "Transform"
  And the graph should contain a CALLS edge from "Process" to "Format"
  And each CALLS edge should have a confidence score of 1.0

Scenario: Index rebuilds symbols when file content changes
  Given the repository has an existing graph database
  And "src/main.go" was previously indexed with function "OldFunc"
  And a new commit renames "OldFunc" to "NewFunc" in "src/main.go"
  When I run "git-agent graph index"
  Then the graph should not contain a Symbol node for "OldFunc"
  And the graph should contain a Symbol node for "NewFunc"
  And CALLS edges referencing "OldFunc" should be removed
  And new CALLS edges for "NewFunc" should be created
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Modify: `application/graph_service_test.go` (add AST indexing test cases)

## Steps

### Step 1: Write AST indexing integration tests

- `TestGraphService_Index_WithAST`: When IndexRequest.AST=true, verify symbols are extracted and stored as Symbol nodes with CONTAINS edges
- `TestGraphService_Index_CallsEdges`: Verify CALLS edges created from AST analysis
- `TestGraphService_Index_ImportsEdges`: Verify IMPORTS edges created from AST analysis

### Step 2: Write symbol rebuild tests

- `TestGraphService_Index_SymbolRebuild`: On incremental index, changed files have old symbols deleted and new ones created (DELETE+CREATE strategy)
- `TestGraphService_Index_SymbolRebuild_CallsUpdated`: Old CALLS edges removed, new ones created after rename

### Step 3: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./application/... -run "TestGraphService_Index_(WithAST|CallsEdges|ImportsEdges|SymbolRebuild)"` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./application/... -run "TestGraphService_Index_(WithAST|CallsEdges|ImportsEdges|SymbolRebuild)" -v
```

## Success Criteria

- Tests verify Symbol node creation during indexing
- Tests verify CALLS and IMPORTS edge creation
- Tests verify DELETE+CREATE rebuild on file changes
- All tests FAIL (Red phase)
