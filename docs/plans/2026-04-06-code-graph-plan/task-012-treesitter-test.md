# Task 012: Tree-sitter Parser Test

**depends-on**: task-002

## Description

Write tests for the Tree-sitter parser infrastructure: language detection from file extension, parsing source code into ASTs, and extracting symbols (functions, classes, methods), call relationships, and import paths across Go, TypeScript, Python, Rust, and Java.

## Execution Context

**Task Number**: 012 of 020 (test)
**Phase**: P1 - AST Layer
**Prerequisites**: ASTParser interface from task-002

## BDD Scenario

```gherkin
Scenario: Index detects and parses multiple languages
  Given the repository contains files:
      | path              | language   |
      | main.go           | Go         |
      | src/app.ts        | TypeScript |
      | lib/utils.py      | Python     |
      | core/engine.rs    | Rust       |
      | api/Handler.java  | Java       |
  When I run "git-agent graph index"
  Then the graph should contain Symbol nodes extracted from each file
  And each File node should have its "language" property set correctly
  And Go functions should be extracted from "main.go"
  And TypeScript classes and functions should be extracted from "src/app.ts"
  And Python function definitions should be extracted from "lib/utils.py"
  And Rust function items should be extracted from "core/engine.rs"
  And Java method declarations should be extracted from "api/Handler.java"

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

Scenario: Index extracts IMPORTS relationships
  Given the repository contains a TypeScript file "src/app.ts" with content:
      """
      import { helper } from './utils';
      import { format } from '../lib/format';
      """
  And the repository contains "src/utils.ts" and "lib/format.ts"
  When I run "git-agent graph index"
  Then the graph should contain an IMPORTS edge from "src/app.ts" to "src/utils.ts"
  And the graph should contain an IMPORTS edge from "src/app.ts" to "lib/format.ts"
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing - multi-language, CALLS, IMPORTS)

## Files to Modify/Create

- Create: `infrastructure/treesitter/parser_test.go` - Create: `infrastructure/treesitter/extractor_test.go` 
## Steps

### Step 1: Write language detection tests

- `TestParser_DetectLanguage`: Maps file extensions to language names (.go -> Go, .ts -> TypeScript, .py -> Python, .rs -> Rust, .java -> Java)
- `TestParser_SupportedLanguages`: Returns at least ["Go", "TypeScript", "Python", "Rust", "Java"]
- `TestParser_UnsupportedLanguage`: Returns error for unknown extensions

### Step 2: Write symbol extraction tests per language

For each supported language, provide small code samples and verify extracted symbols:
- `TestExtractor_Go_Functions`: Extract function declarations
- `TestExtractor_TypeScript_ClassesAndFunctions`: Extract class and function declarations
- `TestExtractor_Python_Functions`: Extract def statements
- `TestExtractor_Rust_Functions`: Extract fn items
- `TestExtractor_Java_Methods`: Extract method declarations

### Step 3: Write CALLS extraction tests

- `TestExtractor_Go_Calls`: From the Process/Transform/Format example, extract CALLS edges with confidence 1.0
- `TestExtractor_TypeScript_Calls`: Function call extraction from TypeScript

### Step 4: Write IMPORTS extraction tests

- `TestExtractor_TypeScript_Imports`: Extract import paths and resolve to file paths
- `TestExtractor_Go_Imports`: Extract Go package imports
- `TestExtractor_Python_Imports`: Extract Python import/from statements

### Step 5: Verify tests fail (Red)

- **Verification**: `go test ./infrastructure/treesitter/... -run "Test(Parser|Extractor)"` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test ./infrastructure/treesitter/... -v
```

## Success Criteria

- Tests cover 5 languages with code samples
- Symbol extraction tests for functions, classes, methods
- CALLS extraction with confidence scores
- IMPORTS extraction with path resolution
- All tests FAIL (Red phase)
