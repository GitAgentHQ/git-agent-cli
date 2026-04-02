# Graph Feature Research: KuzuDB + Tree-sitter for Code Intelligence

## Table of Contents

1. [KuzuDB Best Practices](#1-kuzudb-best-practices)
2. [Tree-sitter Query Patterns](#2-tree-sitter-query-patterns)
3. [BDD Specifications](#3-bdd-specifications)

---

## 1. KuzuDB Best Practices

### 1.1 Project Status and Fork Landscape

The original KuzuDB was archived by Kuzu Inc. in October 2025. Two viable paths exist:

- **Original `kuzudb/kuzu`** (archived): v0.8.x, stable API, single-writer concurrency.
  `go-kuzu` v0.11.3 wraps the C API and is also archived but functional.
- **`Vela-Engineering/kuzu`** (active fork): Adds concurrent multi-writer support
  for multi-agent architectures. MIT-licensed, production-tested.

**Recommendation for git-agent**: Use the original `go-kuzu` v0.11.3. git-agent is a
single-process CLI tool -- concurrent multi-writer is unnecessary. Pin the version
and vendor the dependency to insulate from archive status.

### 1.2 Schema Design for Software Engineering Graphs

KuzuDB uses a structured property graph model requiring pre-defined schema via DDL.
All property types must be explicitly declared. Node tables require a primary key
(STRING, numeric, DATE, or BLOB). Relationship tables derive their keys implicitly
from the connected node primary keys.

#### Node Tables

```cypher
-- Core entities
CREATE NODE TABLE IF NOT EXISTS Commit(
    hash STRING PRIMARY KEY,
    message STRING,
    author_name STRING,
    author_email STRING,
    timestamp INT64,
    parent_hashes STRING[]
);

CREATE NODE TABLE IF NOT EXISTS File(
    path STRING PRIMARY KEY,
    language STRING,
    last_indexed_hash STRING
);

CREATE NODE TABLE IF NOT EXISTS Symbol(
    id STRING PRIMARY KEY,          -- "path:type:name" composite key
    name STRING,
    kind STRING,                    -- function, method, class, interface, type_alias
    file_path STRING,
    start_line INT64,
    end_line INT64,
    signature STRING
);

CREATE NODE TABLE IF NOT EXISTS Author(
    email STRING PRIMARY KEY,
    name STRING
);
```

#### Relationship Tables

```cypher
-- Git history edges
CREATE REL TABLE IF NOT EXISTS AUTHORED(FROM Author TO Commit);

CREATE REL TABLE IF NOT EXISTS MODIFIES(
    FROM Commit TO File,
    additions INT64,
    deletions INT64,
    status STRING                   -- A (added), M (modified), D (deleted), R (renamed)
);

-- AST edges
CREATE REL TABLE IF NOT EXISTS CONTAINS(FROM File TO Symbol);

CREATE REL TABLE IF NOT EXISTS CALLS(
    FROM Symbol TO Symbol,
    confidence DOUBLE               -- 1.0 exact, 0.8 receiver method, 0.5 fuzzy
);

CREATE REL TABLE IF NOT EXISTS IMPORTS(
    FROM File TO File,
    import_path STRING
);

-- Derived edges (computed post-index)
CREATE REL TABLE IF NOT EXISTS CO_CHANGED(
    FROM File TO File,
    coupling_count INT64,
    coupling_strength DOUBLE,       -- coupling_count / max(commits_a, commits_b)
    last_coupled_hash STRING
);
```

**Design rationale**:
- `SERIAL` primary keys are avoided in favor of natural keys (commit hash,
  file path, composite symbol ID) to enable idempotent MERGE operations.
- `CO_CHANGED` is a derived edge, computed from commit history after indexing.
  Coupling strength follows CodeScene's methodology: count co-occurrences in the
  same commit, divide by the maximum individual commit count of either file.
- `CALLS` edges carry confidence scores following the Axon pattern: 1.0 for
  exact static calls, 0.8 for receiver/method dispatch, 0.5 for fuzzy matches.

### 1.3 Cypher Query Patterns for Co-Change Analysis

#### Computing Co-Change Pairs

```cypher
-- Find all file pairs modified in the same commit
MATCH (f1:File)<-[:MODIFIES]-(c:Commit)-[:MODIFIES]->(f2:File)
WHERE f1.path < f2.path  -- avoid counting pairs twice
WITH f1, f2, COUNT(c) AS co_count
WHERE co_count >= 3       -- minimum threshold to filter noise
RETURN f1.path, f2.path, co_count
ORDER BY co_count DESC;
```

#### Materializing CO_CHANGED Edges

```cypher
-- Step 1: Count individual file commits
MATCH (c:Commit)-[:MODIFIES]->(f:File)
WITH f, COUNT(c) AS total_commits
SET f.commit_count = total_commits;

-- Step 2: Create/update CO_CHANGED edges
MATCH (f1:File)<-[:MODIFIES]-(c:Commit)-[:MODIFIES]->(f2:File)
WHERE f1.path < f2.path
WITH f1, f2, COUNT(c) AS co_count
WHERE co_count >= 3
MERGE (f1)-[r:CO_CHANGED]->(f2)
ON CREATE SET
    r.coupling_count = co_count,
    r.coupling_strength = toFloat(co_count) / toFloat(
        CASE WHEN f1.commit_count > f2.commit_count
             THEN f1.commit_count ELSE f2.commit_count END
    )
ON MATCH SET
    r.coupling_count = co_count,
    r.coupling_strength = toFloat(co_count) / toFloat(
        CASE WHEN f1.commit_count > f2.commit_count
             THEN f1.commit_count ELSE f2.commit_count END
    );
```

#### Blast Radius Query (File Level)

```cypher
-- Given a changed file, find all affected files via co-change AND call chains
-- Depth 1: direct co-changes and direct call dependencies
MATCH (target:File {path: $filePath})
OPTIONAL MATCH (target)-[cc:CO_CHANGED]-(cochanged:File)
WHERE cc.coupling_strength >= 0.3
OPTIONAL MATCH (target)-[:CONTAINS]->(sym:Symbol)-[:CALLS]->(callee:Symbol)<-[:CONTAINS]-(caller_file:File)
WHERE caller_file.path <> target.path
RETURN DISTINCT
    coalesce(cochanged.path, caller_file.path) AS affected_path,
    CASE
        WHEN cochanged IS NOT NULL AND caller_file IS NOT NULL THEN 'co-change+call'
        WHEN cochanged IS NOT NULL THEN 'co-change'
        ELSE 'call-dependency'
    END AS reason,
    cc.coupling_strength AS strength
ORDER BY strength DESC;
```

#### Blast Radius Query (Symbol Level, Multi-Hop)

```cypher
-- Find all symbols reachable from a given symbol via CALLS edges (up to 3 hops)
MATCH (target:Symbol {name: $symbolName, file_path: $filePath})
MATCH (target)-[:CALLS*1..3]->(affected:Symbol)
RETURN DISTINCT
    affected.file_path AS file,
    affected.name AS symbol,
    affected.kind AS kind,
    length(shortestPath((target)-[:CALLS*]->(affected))) AS depth
ORDER BY depth, file;
```

### 1.4 Performance Tips for Embedded Use

#### Buffer Pool Configuration

```go
config := kuzu.DefaultSystemConfig()
// For CLI tools indexing typical repos (<100K files):
// 256MB is sufficient. Default (80% RAM) is wasteful for a CLI tool.
config.BufferPoolSize = 256 * 1024 * 1024  // 256 MB
config.MaxNumThreads = 4                    // limit parallelism for CLI
config.EnableCompression = true             // smaller on-disk footprint

db, err := kuzu.OpenDatabase(dbPath, config)
```

**Guidelines**:
- **256 MB buffer pool** is appropriate for repositories up to ~100K files.
  For monorepos with >500K files, increase to 512 MB-1 GB.
- **Compression**: Always enable. Reduces disk I/O which dominates embedded
  workloads. KuzuDB's columnar storage benefits significantly.
- **Thread count**: Cap at 4 for CLI tools. The user's machine is shared;
  saturating all cores degrades the interactive experience.
- **Read-only mode**: Open with `config.ReadOnly = true` for query-only
  commands (`graph query`). Allows concurrent reads without lock contention.

#### Query Optimization

- Use `EXPLAIN` and `PROFILE` during development to inspect execution plans.
- KuzuDB uses zone maps on numeric columns -- filtering on `timestamp`,
  `start_line`, `coupling_count` benefits from automatic skip-scan.
- For variable-length path queries (`*1..3`), always bound the max depth.
  Unbounded traversals can explode on dense graphs.
- Prefer `shortestPath()` over `*1..N` when only the minimum distance matters.

#### Bulk Import vs. Incremental

- **First-time full index**: Use `COPY FROM` with CSV/Parquet files for
  initial bulk load. KuzuDB's COPY is 53x faster than row-by-row CREATE
  (benchmarked at 100K nodes, 2.4M edges in 0.58s vs 30.64s for Neo4j).
- **Incremental updates**: Use `MERGE` with `ON CREATE`/`ON MATCH` for
  sporadic additions after the initial index.

### 1.5 Incremental Updates: MERGE vs DELETE+CREATE

#### MERGE (Preferred for Incremental)

```cypher
-- Upsert a commit node
MERGE (c:Commit {hash: $hash})
ON CREATE SET
    c.message = $message,
    c.author_name = $authorName,
    c.author_email = $authorEmail,
    c.timestamp = $timestamp
ON MATCH SET
    c.message = $message;

-- Upsert a symbol (idempotent on composite key)
MERGE (s:Symbol {id: $id})
ON CREATE SET
    s.name = $name,
    s.kind = $kind,
    s.file_path = $filePath,
    s.start_line = $startLine,
    s.end_line = $endLine,
    s.signature = $signature
ON MATCH SET
    s.start_line = $startLine,
    s.end_line = $endLine,
    s.signature = $signature;
```

**MERGE semantics in KuzuDB**: The entire pattern must match or the entire
pattern is created. There is no partial matching. This makes it safe for
idempotent indexing -- re-running on the same commit is a no-op.

#### DELETE+CREATE (For File Re-indexing)

When a file's AST changes, its symbols and relationships must be rebuilt.
DELETE+CREATE is cleaner than attempting to diff individual symbols:

```cypher
-- Delete old symbols and their edges for a file
MATCH (f:File {path: $filePath})-[:CONTAINS]->(s:Symbol)
DETACH DELETE s;

-- Then re-create symbols from fresh parse
CREATE (s:Symbol {id: $id, name: $name, kind: $kind, ...});
MATCH (f:File {path: $filePath}), (s:Symbol {id: $id})
CREATE (f)-[:CONTAINS]->(s);
```

**Recommendation**: Use a hybrid approach:
1. **Commits, Authors, MODIFIES, AUTHORED**: Always MERGE (append-only data).
2. **Symbols, CONTAINS, CALLS, IMPORTS**: DELETE+CREATE per file when the file
   hash changes (AST must be fully reparsed anyway).
3. **CO_CHANGED**: Recompute after all new commits are indexed. Use MERGE with
   ON MATCH to update counts.

### 1.6 Incremental Indexing State

Track the last indexed commit to enable incremental updates:

```cypher
CREATE NODE TABLE IF NOT EXISTS IndexState(
    key STRING PRIMARY KEY,
    value STRING
);

-- After indexing:
MERGE (s:IndexState {key: 'last_indexed_commit'})
ON CREATE SET s.value = $commitHash
ON MATCH SET s.value = $commitHash;

-- Before indexing, read the watermark:
MATCH (s:IndexState {key: 'last_indexed_commit'})
RETURN s.value AS lastHash;
```

Use `git log $lastHash..HEAD` to discover new commits for incremental indexing.

---

## 2. Tree-sitter Query Patterns

### 2.1 Library Choice: gotreesitter (pure Go)

> **Decision**: Use `github.com/drummonds/gotreesitter` v0.6.4 (pure Go, zero CGo).
> See [_index.md](./_index.md) for the approved approach.

The `gotreesitter` library provides a pure-Go tree-sitter runtime with 206
grammars. No CGo, no C toolchain required. It supports:

- Parser creation and language selection
- S-expression query compilation and execution
- TreeCursor-based traversal
- Predicate filtering on captures

The S-expression queries in section 2.3 below are tree-sitter standard and work
with any tree-sitter runtime (CGo or pure Go). The query syntax is identical.

### 2.2 Node Type Reference by Language

Each language has its own AST node types. The key nodes for code intelligence:

| Concept | Go | TypeScript | Python | Rust | Java |
|---|---|---|---|---|---|
| Function def | `function_declaration` | `function_declaration` | `function_definition` | `function_item` | `method_declaration` |
| Method def | `method_declaration` | `method_definition` | `function_definition` (in class) | `function_item` (in `impl_item`) | `method_declaration` |
| Class def | `type_declaration` (struct) | `class_declaration` | `class_definition` | `struct_item` / `impl_item` | `class_declaration` |
| Interface | `type_declaration` (interface) | `interface_declaration` | `class_definition` (Protocol) | `trait_item` | `interface_declaration` |
| Function call | `call_expression` | `call_expression` | `call` | `call_expression` | `method_invocation` |
| Import | `import_declaration` | `import_statement` | `import_statement` / `import_from_statement` | `use_declaration` | `import_declaration` |
| Package/Module | `package_clause` | N/A (file-level) | N/A (file-level) | `mod_item` | `package_declaration` |

### 2.3 S-Expression Queries for Symbol Extraction

#### Go

```scheme
;; Function declarations
(function_declaration
    name: (identifier) @func.name
    parameters: (parameter_list) @func.params
    result: (_)? @func.return
    body: (block) @func.body) @func.def

;; Method declarations (receiver)
(method_declaration
    receiver: (parameter_list
        (parameter_declaration
            type: (_) @method.receiver_type))
    name: (field_identifier) @method.name
    parameters: (parameter_list) @method.params) @method.def

;; Call expressions
(call_expression
    function: (identifier) @call.name
    arguments: (argument_list) @call.args) @call.expr

;; Method calls (selector)
(call_expression
    function: (selector_expression
        operand: (_) @call.receiver
        field: (field_identifier) @call.method)
    arguments: (argument_list) @call.args) @call.expr

;; Import declarations
(import_declaration
    (import_spec_list
        (import_spec
            path: (interpreted_string_literal) @import.path
            name: (package_identifier)? @import.alias))) @import.decl

;; Type declarations (struct/interface)
(type_declaration
    (type_spec
        name: (type_identifier) @type.name
        type: (struct_type) @type.body)) @type.struct

(type_declaration
    (type_spec
        name: (type_identifier) @type.name
        type: (interface_type) @type.body)) @type.interface
```

#### TypeScript

```scheme
;; Function declarations
(function_declaration
    name: (identifier) @func.name
    parameters: (formal_parameters) @func.params
    return_type: (_)? @func.return
    body: (statement_block) @func.body) @func.def

;; Arrow functions assigned to variables
(lexical_declaration
    (variable_declarator
        name: (identifier) @func.name
        value: (arrow_function
            parameters: (formal_parameters) @func.params
            body: (_) @func.body))) @func.def

;; Class declarations
(class_declaration
    name: (type_identifier) @class.name
    body: (class_body) @class.body) @class.def

;; Method definitions inside classes
(method_definition
    name: (property_identifier) @method.name
    parameters: (formal_parameters) @method.params
    body: (statement_block) @method.body) @method.def

;; Call expressions
(call_expression
    function: (identifier) @call.name
    arguments: (arguments) @call.args) @call.expr

;; Member call expressions (obj.method())
(call_expression
    function: (member_expression
        object: (_) @call.receiver
        property: (property_identifier) @call.method)
    arguments: (arguments) @call.args) @call.expr

;; Import statements
(import_statement
    source: (string) @import.path) @import.decl
```

#### Python

```scheme
;; Function definitions
(function_definition
    name: (identifier) @func.name
    parameters: (parameters) @func.params
    return_type: (_)? @func.return
    body: (block) @func.body) @func.def

;; Class definitions
(class_definition
    name: (identifier) @class.name
    body: (block) @class.body) @class.def

;; Call expressions
(call
    function: (identifier) @call.name
    arguments: (argument_list) @call.args) @call.expr

;; Attribute call (obj.method())
(call
    function: (attribute
        object: (_) @call.receiver
        attribute: (identifier) @call.method)
    arguments: (argument_list) @call.args) @call.expr

;; Import statements
(import_statement
    name: (dotted_name) @import.module) @import.decl

(import_from_statement
    module_name: (dotted_name) @import.module
    name: (dotted_name) @import.name) @import.decl
```

#### Rust

```scheme
;; Function items
(function_item
    name: (identifier) @func.name
    parameters: (parameters) @func.params
    return_type: (_)? @func.return
    body: (block) @func.body) @func.def

;; Impl blocks (capture the type being implemented)
(impl_item
    type: (_) @impl.type
    body: (declaration_list) @impl.body) @impl.def

;; Call expressions
(call_expression
    function: (identifier) @call.name
    arguments: (arguments) @call.args) @call.expr

;; Method call (receiver.method())
(call_expression
    function: (field_expression
        value: (_) @call.receiver
        field: (field_identifier) @call.method)
    arguments: (arguments) @call.args) @call.expr

;; Use declarations (imports)
(use_declaration
    argument: (_) @import.path) @import.decl
```

#### Java

```scheme
;; Method declarations
(method_declaration
    name: (identifier) @method.name
    parameters: (formal_parameters) @method.params
    body: (block) @method.body) @method.def

;; Class declarations
(class_declaration
    name: (identifier) @class.name
    body: (class_body) @class.body) @class.def

;; Method invocations
(method_invocation
    name: (identifier) @call.method
    arguments: (argument_list) @call.args) @call.expr

;; Import declarations
(import_declaration
    (scoped_identifier) @import.path) @import.decl
```

### 2.4 Mapping AST Nodes to Graph Nodes

The extraction pipeline converts tree-sitter captures to graph entities:

```
AST Parse Result               Graph Entity
---------------------          -------------------------
@func.def capture        -->   Symbol node (kind="function")
  @func.name text        -->     Symbol.name
  @func.params text      -->     Symbol.signature (partial)
  @func.def byte range   -->     Symbol.start_line, Symbol.end_line

@call.expr capture       -->   CALLS edge
  @call.name text        -->     target Symbol lookup by name
  @call.receiver text    -->     scope narrowing for method dispatch

@import.decl capture     -->   IMPORTS edge
  @import.path text      -->     target File lookup by resolved path
```

**Key mapping decisions**:

1. **Symbol ID construction**: `"{file_path}:{kind}:{name}:{start_line}"` as a composite
   primary key. For methods, include the receiver type:
   `"pkg/foo.go:method:Bar.Baz:42"`.

2. **Call resolution strategy**: Static calls resolve by name matching within
   import scope. Method calls resolve by receiver type + method name. Assign
   confidence scores: 1.0 for unambiguous static calls, 0.8 for receiver
   method dispatch (type could be interface), 0.5 for same-name different-package.

3. **Import path resolution**: Convert language-specific import syntax to
   file paths relative to the repository root. For Go, resolve via module path.
   For TypeScript/Python, resolve relative/absolute imports. Store both the raw
   import string and resolved path.

4. **Scope nesting**: Methods inside classes/impls are linked to their parent
   symbol. The `CONTAINS` edge from File to Symbol handles the primary
   containment. For nested symbols, add a `DEFINES` edge from parent Symbol
   to child Symbol (stretch goal, not in initial schema).

### 2.5 Language Detection Strategy

Use file extension mapping with fallback to tree-sitter language detection:

```
.go          -> Go
.ts, .tsx    -> TypeScript
.js, .jsx    -> JavaScript (use TypeScript parser, it handles JS)
.py          -> Python
.rs          -> Rust
.java        -> Java
.rb          -> Ruby
.c, .h       -> C
.cpp, .hpp   -> C++
.cs          -> C#
.swift       -> Swift
.kt          -> Kotlin
```

For the initial implementation, prioritize Go, TypeScript, Python, Rust, and Java.
These cover the dominant languages in the coding agent ecosystem.

---

## 3. BDD Specifications

### Feature: Graph Indexing

```gherkin
Feature: Graph Indexing
    As a coding agent
    I want to index a git repository into a graph database
    So that I can query code relationships and change patterns

    Background:
        Given a git repository at "/tmp/test-repo"
        And the repository has commits with source files

    Scenario: First-time full index of a git repository
        Given the repository has no existing graph database
        And the repository has 3 commits modifying 5 files
        When I run "git-agent graph index"
        Then a graph database should be created at ".git-agent/graph.db"
        And the graph should contain 3 Commit nodes
        And the graph should contain 5 File nodes
        And the graph should contain Author nodes for each unique committer
        And the graph should contain MODIFIES edges linking commits to files
        And the graph should contain AUTHORED edges linking authors to commits
        And the IndexState should record the latest commit hash
        And the command should exit with code 0

    Scenario: Incremental index after new commits
        Given the repository has an existing graph database
        And the IndexState records commit "abc1234" as last indexed
        And 2 new commits exist after "abc1234"
        And the new commits modify 3 files
        When I run "git-agent graph index"
        Then only the 2 new commits should be indexed
        And the graph should contain the previously indexed data unchanged
        And the new Commit nodes and MODIFIES edges should be added
        And the IndexState should be updated to the latest commit hash
        And the command should report "indexed 2 new commits"

    Scenario: Incremental index is idempotent
        Given the repository has an existing graph database
        And the IndexState records the latest commit as last indexed
        When I run "git-agent graph index"
        Then no new data should be added to the graph
        And the command should report "already up to date"
        And the command should exit with code 0

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

    Scenario: Index handles large repositories gracefully
        Given the repository has 10000 commits modifying 5000 files
        When I run "git-agent graph index"
        Then the indexing should complete without running out of memory
        And the buffer pool should be configured to 256 MB
        And bulk import should be used for the initial load
        And the command should report progress during indexing
        And the total indexing time should be under 120 seconds

    Scenario: Index skips binary and vendor files
        Given the repository contains files:
            | path                   | type    |
            | src/main.go            | source  |
            | vendor/lib/dep.go      | vendor  |
            | assets/logo.png        | binary  |
            | node_modules/pkg/x.js  | vendor  |
            | go.sum                 | lockfile|
        When I run "git-agent graph index"
        Then the graph should contain a File node for "src/main.go"
        And the graph should not contain File nodes for vendor directories
        And the graph should not contain File nodes for binary files
        And the graph should not contain File nodes for lock files

    Scenario: Index computes CO_CHANGED edges
        Given the repository has commits where "a.go" and "b.go" are modified together 5 times
        And "a.go" has been modified 8 times total
        And "b.go" has been modified 6 times total
        When I run "git-agent graph index"
        Then the graph should contain a CO_CHANGED edge from "a.go" to "b.go"
        And the edge should have coupling_count of 5
        And the edge should have coupling_strength of approximately 0.625
        And pairs with fewer than 3 co-changes should not have CO_CHANGED edges

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

### Feature: Blast Radius Query

```gherkin
Feature: Blast Radius Query
    As a coding agent
    I want to query the blast radius of a code change
    So that I can understand what files and symbols are affected

    Background:
        Given a git repository with an indexed graph database
        And the graph contains the following structure:
            | File         | Symbols                    |
            | api/handler.go | HandleRequest, Validate  |
            | pkg/service.go | Process, Transform       |
            | pkg/utils.go   | Format, Sanitize         |
            | db/store.go    | Save, Load               |
            | config/cfg.go  | ReadConfig               |
        And the following CALLS relationships exist:
            | caller        | callee      |
            | HandleRequest | Process     |
            | HandleRequest | Validate    |
            | Process       | Transform   |
            | Process       | Save        |
            | Transform     | Format      |
            | Transform     | Sanitize    |
        And the following CO_CHANGED relationships exist:
            | file1          | file2          | strength |
            | pkg/service.go | db/store.go    | 0.7      |
            | pkg/utils.go   | pkg/service.go | 0.5      |

    Scenario: Query blast radius of a single file (co-change + call chain)
        When I run "git-agent graph blast-radius --file pkg/service.go"
        Then the output should list affected files:
            | file            | reason          | depth |
            | db/store.go     | co-change       | 1     |
            | pkg/utils.go    | co-change       | 1     |
            | api/handler.go  | call-dependency | 1     |
        And each result should include the reason for impact
        And co-change results should include coupling strength

    Scenario: Query blast radius of a specific function/symbol
        When I run "git-agent graph blast-radius --symbol Transform --file pkg/service.go"
        Then the output should list affected symbols by call chain depth:
            | symbol        | file            | depth |
            | Format        | pkg/utils.go    | 1     |
            | Sanitize      | pkg/utils.go    | 1     |
        And upstream callers should also be listed:
            | symbol        | file            | depth |
            | Process       | pkg/service.go  | 1     |
            | HandleRequest | api/handler.go  | 2     |

    Scenario: Query blast radius with depth limit
        When I run "git-agent graph blast-radius --symbol HandleRequest --file api/handler.go --depth 1"
        Then the output should only include symbols at depth 1:
            | symbol    | file           |
            | Process   | pkg/service.go |
            | Validate  | api/handler.go |
        And symbols beyond depth 1 should not appear in the results

    Scenario: Query returns empty result for isolated file
        Given the file "config/cfg.go" has no CALLS edges to other files
        And "config/cfg.go" has no CO_CHANGED edges above the threshold
        When I run "git-agent graph blast-radius --file config/cfg.go"
        Then the output should indicate no blast radius detected
        And the exit code should be 0

    Scenario: Agent queries via CLI and gets JSON output
        When I run "git-agent graph blast-radius --file pkg/service.go --format json"
        Then the output should be valid JSON
        And the JSON should contain an array of affected items
        And each item should have fields: "path", "symbol", "reason", "depth", "confidence"
        And the JSON should include a "query_time_ms" metadata field

    Scenario: Blast radius includes transitive co-changes
        Given "a.go" co-changes with "b.go" (strength 0.8)
        And "b.go" co-changes with "c.go" (strength 0.6)
        When I run "git-agent graph blast-radius --file a.go --depth 2"
        Then "b.go" should appear at depth 1
        And "c.go" should appear at depth 2
        And deeper transitive co-changes should not appear

    Scenario: Blast radius query on non-existent file
        When I run "git-agent graph blast-radius --file nonexistent.go"
        Then the command should exit with code 1
        And the error message should indicate the file is not in the graph
```

### Feature: Code Ownership Query

```gherkin
Feature: Code Ownership Query
    As a coding agent
    I want to query who owns or maintains a file or module
    So that I can identify the right people for code review

    Background:
        Given a git repository with an indexed graph database
        And the following commit history exists:
            | author          | file           | commits |
            | alice@dev.com   | pkg/service.go | 15      |
            | bob@dev.com     | pkg/service.go | 8       |
            | carol@dev.com   | pkg/service.go | 3       |
            | alice@dev.com   | pkg/utils.go   | 2       |
            | bob@dev.com     | pkg/utils.go   | 20      |
            | carol@dev.com   | db/store.go    | 25      |

    Scenario: Query who owns a file (most commits)
        When I run "git-agent graph ownership --file pkg/service.go"
        Then the output should list authors ordered by commit count:
            | author          | commits | percentage |
            | alice@dev.com   | 15      | 57.7%      |
            | bob@dev.com     | 8       | 30.8%      |
            | carol@dev.com   | 3       | 11.5%      |
        And the primary owner should be "alice@dev.com"

    Scenario: Query recent maintainers of a module
        Given the following recent commit history (last 90 days):
            | author          | file           | commits |
            | bob@dev.com     | pkg/service.go | 6       |
            | alice@dev.com   | pkg/service.go | 1       |
        When I run "git-agent graph ownership --path pkg/ --since 90d"
        Then the output should list recent active maintainers for the module
        And "bob@dev.com" should be ranked first for recent activity
        And the output should distinguish between all-time and recent ownership

    Scenario: Query ownership for a directory (module level)
        When I run "git-agent graph ownership --path pkg/"
        Then the output should aggregate ownership across all files in "pkg/"
        And the output should list the top contributors to the module
        And each contributor should show their file-level breakdown

    Scenario: Query ownership with JSON output
        When I run "git-agent graph ownership --file pkg/service.go --format json"
        Then the output should be valid JSON
        And each entry should have fields: "email", "name", "commits", "percentage", "last_active"

    Scenario: Query ownership for file with single author
        Given "solo.go" has only been modified by "alice@dev.com"
        When I run "git-agent graph ownership --file solo.go"
        Then the output should show "alice@dev.com" as the sole owner at 100%
```

### Feature: Change Pattern Query

```gherkin
Feature: Change Pattern Query
    As a coding agent
    I want to query change frequency and stability metrics
    So that I can identify hotspots and assess code health

    Background:
        Given a git repository with an indexed graph database
        And the repository spans 6 months of commit history

    Scenario: Query change frequency / hotspots
        When I run "git-agent graph hotspots"
        Then the output should list files ordered by change frequency:
            | file            | changes | last_changed    |
            | pkg/service.go  | 45      | 2026-03-28      |
            | api/handler.go  | 38      | 2026-04-01      |
            | pkg/utils.go    | 12      | 2026-03-15      |
        And the output should highlight the top 10 hotspots by default
        And each file should show its total change count and last modification date

    Scenario: Query hotspots with time window
        When I run "git-agent graph hotspots --since 30d"
        Then only changes from the last 30 days should be counted
        And files unchanged in that period should not appear
        And the output should indicate the time window used

    Scenario: Query stability metrics for a module
        When I run "git-agent graph stability --path pkg/"
        Then the output should include:
            | metric                  | value  |
            | total_files             | 5      |
            | total_changes           | 120    |
            | avg_changes_per_file    | 24.0   |
            | max_changes_single_file | 45     |
            | unique_contributors     | 4      |
            | churn_rate              | 2.8/wk |
            | co_change_clusters      | 2      |
        And the churn rate should be changes per week over the analysis period
        And co-change clusters should identify groups of files that change together

    Scenario: Query stability for a single file
        When I run "git-agent graph stability --file pkg/service.go"
        Then the output should include file-specific metrics:
            | metric              | value       |
            | total_changes       | 45          |
            | unique_contributors | 3           |
            | avg_change_size     | 15 lines    |
            | last_30d_changes    | 8           |
            | co_changed_files    | 3           |
            | primary_owner       | alice@dev   |

    Scenario: Query change patterns with JSON output
        When I run "git-agent graph hotspots --format json --limit 5"
        Then the output should be valid JSON
        And the JSON should contain at most 5 entries
        And each entry should have fields: "path", "changes", "last_changed", "contributors"

    Scenario: Query hotspots in empty or freshly initialized repository
        Given the repository has only 1 commit (initial)
        When I run "git-agent graph hotspots"
        Then all files should show a change count of 1
        And the output should note the limited history

    Scenario: Query identifies co-change clusters
        Given the following co-change patterns exist:
            | cluster | files                                       |
            | A       | api/handler.go, pkg/service.go, db/store.go |
            | B       | config/cfg.go, config/env.go                |
        When I run "git-agent graph clusters"
        Then the output should group files into co-change clusters
        And cluster A should contain the API-service-database chain
        And cluster B should contain the configuration files
        And each cluster should show internal coupling strength

    Scenario: Hotspot query excludes generated and test files
        When I run "git-agent graph hotspots --exclude-tests --exclude-generated"
        Then files matching "*_test.go", "*.test.ts", "test_*.py" should be excluded
        And files matching "*.generated.go", "*.pb.go" should be excluded
        And only production source files should appear in results
```

---

## Sources

### KuzuDB
- [KuzuDB GitHub Repository](https://github.com/kuzudb/kuzu)
- [go-kuzu Go Bindings](https://github.com/kuzudb/go-kuzu)
- [go-kuzu API Reference](https://pkg.go.dev/github.com/kuzudb/go-kuzu)
- [KuzuDB Cypher Manual](https://docs.kuzudb.com/cypher/)
- [KuzuDB MERGE Clause](https://docs.kuzudb.com/cypher/data-manipulation-clauses/merge/)
- [KuzuDB CREATE TABLE DDL](https://docs.kuzudb.com/cypher/data-definition/create-table/)
- [KuzuDB Performance Debugging](https://docs.kuzudb.com/developer-guide/performance-debugging/)
- [KuzuDB vs Neo4j Differences](https://docs.kuzudb.com/cypher/difference/)
- [KuzuDB Benchmark Study](https://github.com/prrao87/kuzudb-study)
- [Vela Partners KuzuDB Fork](https://www.vela.partners/blog/kuzudb-ai-agent-memory-graph-database)
- [KuzuDB Embedded Database Analysis](https://thedataquarry.com/blog/embedded-db-2/)
- [Leveraging Kuzu and Cypher for Advanced Data Analysis](https://cu-dbmi.github.io/set-website/2024/05/24/Leveraging-K%C3%B9zu-and-Cypher-for-Advanced-Data-Analysis.html)

### Tree-sitter
- [Tree-sitter Query Syntax](https://tree-sitter.github.io/tree-sitter/using-parsers/queries/1-syntax.html)
- [Knee Deep in Tree-sitter Queries (Go focus)](https://parsiya.net/blog/knee-deep-tree-sitter-queries/)
- [smacker/go-tree-sitter Bindings](https://github.com/smacker/go-tree-sitter)
- [tree-sitter/go-tree-sitter Official Bindings](https://github.com/tree-sitter/go-tree-sitter)
- [tree-sitter-go Node Types](https://github.com/tree-sitter/tree-sitter-go/blob/master/src/node-types.json)
- [tree-sitter-typescript Node Types](https://github.com/tree-sitter/tree-sitter-typescript/blob/master/tsx/src/node-types.json)
- [tree-sitter-java Node Types](https://github.com/tree-sitter/tree-sitter-java/blob/master/src/node-types.json)
- [AST Parsing at Scale with Tree-sitter](https://www.dropstone.io/blog/ast-parsing-tree-sitter-40-languages)
- [CodeRAG with Dependency Graph Using Tree-Sitter](https://medium.com/@shsax/how-i-built-coderag-with-dependency-graph-using-tree-sitter-0a71867059ae)

### Co-Change Analysis
- [CodeScene Change Coupling Guide](https://codescene.io/docs/guides/technical/change-coupling.html)
- [CodeScene Temporal Coupling](https://docs.enterprise.codescene.io/versions/3.2.9/guides/technical/temporal-coupling.html)
- [Temporal Coupling Explorer](https://github.com/shepmaster/temporal-coupling)
- [Axon Code Intelligence Engine](https://github.com/harshkedia177/axon)
- [Variable-Length Cypher Relationships](https://graphaware.com/blog/neo4j-cypher-variable-length-relationships-by-example/)
