# Task 012: Tree-sitter Parser Impl

**depends-on**: task-012-treesitter-test

## Description

Implement the ASTParser interface using `gotreesitter`: language detection, parsing source code into ASTs, extracting symbols/calls/imports using embedded .scm query files per language.

## Execution Context

**Task Number**: 012 of 020 (impl)
**Phase**: P1 - AST Layer
**Prerequisites**: Failing tests from task-012-treesitter-test

## BDD Scenario

```gherkin
Scenario: Index detects and parses multiple languages
  Given the repository contains files in Go, TypeScript, Python, Rust, Java
  When I run "git-agent graph index"
  Then the graph should contain Symbol nodes extracted from each file
  And each File node should have its "language" property set correctly
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Create: `infrastructure/treesitter/parser.go` (with `//go:build graph` tag)
- Create: `infrastructure/treesitter/extractor.go` (with `//go:build graph` tag)
- Create: `infrastructure/treesitter/queries/go.scm`
- Create: `infrastructure/treesitter/queries/typescript.scm`
- Create: `infrastructure/treesitter/queries/python.scm`
- Create: `infrastructure/treesitter/queries/rust.scm`
- Create: `infrastructure/treesitter/queries/java.scm`

## Steps

### Step 1: Implement language detection

Map file extensions to `gotreesitter` grammar identifiers. Return error for unsupported extensions.

### Step 2: Implement Parse method

Use `gotreesitter` to parse source bytes into a syntax tree for the detected language. Run the language-specific .scm query to extract symbols, calls, and imports.

### Step 3: Create .scm query files

Write Tree-sitter query files for each language that capture:
- **Symbols**: function declarations, class declarations, method declarations, interface definitions
- **Calls**: function call expressions with the callee name
- **Imports**: import statements with paths

### Step 4: Implement extractor

Convert Tree-sitter query matches into domain types: SymbolNode (with id, name, kind, file_path, start_line, end_line), CallEdge (from/to with confidence), ImportEdge (from file, import_path).

### Step 5: Verify tests pass (Green)

- **Verification**: `go test -tags graph ./infrastructure/treesitter/... -v` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test -tags graph ./infrastructure/treesitter/... -v
```

## Success Criteria

- 5 languages supported (Go, TypeScript, Python, Rust, Java)
- Symbol extraction works for functions, classes, methods
- CALLS extraction identifies function calls with confidence
- IMPORTS extraction resolves import paths
- All parser and extractor tests pass (Green)
