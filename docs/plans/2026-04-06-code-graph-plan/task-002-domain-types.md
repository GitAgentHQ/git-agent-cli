# Task 002: Domain Types and Interfaces

**depends-on**: task-001

## Description

Define all domain value objects (DTOs) and interfaces for the graph feature. Domain layer has zero external imports -- only pure Go types and interfaces. These define the contracts that infrastructure and application layers implement and consume.

## Execution Context

**Task Number**: 002 of 020
**Phase**: Foundation
**Prerequisites**: Directory structure from task-001

## BDD Scenario

```gherkin
Scenario: Domain types have zero external imports
  Given the domain/graph/ package exists
  When I inspect its import statements
  Then no external dependencies should be imported
  And only standard library packages should be used
  And all interfaces should be defined for consumers to implement
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (Domain Interfaces section)

## Files to Modify/Create

- Create: `domain/graph/repository.go` -- GraphRepository interface
- Create: `domain/graph/nodes.go` -- CommitNode, FileNode, SymbolNode, AuthorNode, SessionNode, ActionNode, IndexState
- Create: `domain/graph/edges.go` -- CallEdge, ImportEdge, CoChangedEntry
- Create: `domain/graph/index.go` -- IndexRequest, IndexResult, IndexStatus DTOs
- Create: `domain/graph/query.go` -- BlastRadiusRequest, BlastRadiusResult, HotspotsRequest, HotspotsResult, OwnershipRequest, OwnershipResult, GraphStats, GraphStatus
- Create: `domain/graph/session.go` -- CaptureRequest, CaptureResult DTOs
- Create: `domain/graph/timeline.go` -- TimelineRequest, TimelineResult DTOs
- Create: `domain/graph/parser.go` -- ASTParser interface, ParseResult

## Steps

### Step 1: Define node types in nodes.go

Create value objects for all graph node types: CommitNode, FileNode, SymbolNode, AuthorNode, SessionNode, ActionNode. Each struct has exported fields matching the SQLite schema columns.

### Step 2: Define edge types in edges.go

Create value objects for edge data: CallEdge (from/to symbol ID + confidence), ImportEdge (from file + import path + resolved path), CoChangedEntry (file pair + coupling count/strength).

### Step 3: Define request/result DTOs

Create DTOs for each operation: IndexRequest/IndexResult, BlastRadiusRequest/BlastRadiusResult, HotspotsRequest/HotspotsResult, OwnershipRequest/OwnershipResult, CaptureRequest/CaptureResult, TimelineRequest/TimelineResult, GraphStats, GraphStatus.

### Step 4: Define GraphRepository interface

Define the full `GraphRepository` interface with lifecycle methods (Open, Close, InitSchema, Drop), write methods (Upsert*, Create*, Replace*, Recompute*), state methods (Get/SetLastIndexedCommit, GetStats), and read methods (BlastRadius, Hotspots, Ownership, Timeline, etc.).

### Step 5: Define ASTParser interface

Define the `ASTParser` interface with `Parse(ctx, language, source) (*ParseResult, error)` and `SupportedLanguages() []string`.

### Step 6: Define GraphGitClient interface

Define the `GraphGitClient` interface extending the existing git client with `CommitLogDetailed`, `FileContentAt`, `CurrentHead`, `Diff`, `DiffFiles`.

### Step 7: Verify compilation

- **Verification**: `go build ./domain/graph/...` compiles with no external imports

## Verification Commands

```bash
# Verify compilation
go build ./domain/graph/...

# Verify no external imports
go list -f '{{.Imports}}' ./domain/graph/ | grep -v "^[" | grep -v "context\|fmt\|time\|strings"
# Should output nothing (only stdlib imports)
```

## Success Criteria

- All DTOs and interfaces defined in `domain/graph/`
- Zero external imports in domain layer
- Package compiles successfully
- All types match the schema defined in the design document
