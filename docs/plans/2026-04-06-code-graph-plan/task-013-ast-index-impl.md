# Task 013: AST Indexing Impl

**depends-on**: task-013-ast-index-test

## Description

Integrate AST parsing into GraphService.Index: when AST flag is set, use the ASTParser to extract symbols, calls, and imports from each modified file, store them in the graph, and implement the DELETE+CREATE strategy for incremental symbol rebuilds.

## Execution Context

**Task Number**: 013 of 020 (impl)
**Phase**: P1 - AST Indexing
**Prerequisites**: Failing tests from task-013-ast-index-test

## BDD Scenario

```gherkin
Scenario: Index rebuilds symbols when file content changes
  Given "src/main.go" was previously indexed with function "OldFunc"
  And a new commit renames "OldFunc" to "NewFunc" in "src/main.go"
  When I run "git-agent graph index"
  Then the graph should not contain a Symbol node for "OldFunc"
  And the graph should contain a Symbol node for "NewFunc"
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Modify: `application/graph_service.go` -- extend Index to include AST parsing
- Modify: `infrastructure/graph/sqlite_repository.go` -- implement ReplaceFileSymbols

## Steps

### Step 1: Extend Index with AST parsing

When `IndexRequest.AST` is true, for each modified file with a supported language:
1. Call `git.FileContentAt(ctx, commitHash, filePath)` to get source
2. Call `parser.Parse(ctx, language, source)` to get ParseResult
3. Call `repo.ReplaceFileSymbols(ctx, filePath, symbols, calls, imports)` to store

### Step 2: Implement ReplaceFileSymbols

DELETE+CREATE strategy: delete all Symbol nodes for the file (and their CONTAINS, CALLS edges), then create new ones. This handles renames and removals correctly.

### Step 3: Handle IMPORTS edges

For each import in ParseResult.Imports, resolve the import path to a file path and create IMPORTS edges between File nodes.

### Step 4: Verify tests pass (Green)

- **Verification**: `go test ./application/... -run "TestGraphService_Index_(WithAST|CallsEdges|ImportsEdges|SymbolRebuild)"` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./application/... -run "TestGraphService_Index_(WithAST|CallsEdges|ImportsEdges|SymbolRebuild)" -v
```

## Success Criteria

- AST parsing integrated into index flow
- Symbols, CALLS, and IMPORTS edges created during indexing
- DELETE+CREATE rebuild works for incremental updates
- All AST indexing tests pass (Green)
