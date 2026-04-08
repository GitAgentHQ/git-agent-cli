# Task 002: Domain types and interfaces

**depends-on**: task-001

## Description
Define all domain types, value objects, and interfaces for the graph feature in `domain/graph/`. This layer has zero external imports -- pure Go only. Also add `CoChangeHint` and `CoChangeHints` to the existing `domain/commit/planner.go` (already done in prior work).

## Execution Context
**Task Number**: 002 of 018
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-001 (directory structure exists)

## BDD Scenario
```gherkin
Feature: Domain types for code graph

  Scenario: Domain graph package has zero external imports
    Given the domain/graph/ package
    When I check all import statements
    Then no import references an external module
    And no import references infrastructure/ or application/

  Scenario: All value objects are defined
    Given the domain/graph/types.go file
    Then CommitNode has fields: Hash, Message, AuthorName, AuthorEmail, Timestamp, ParentHashes
    And FileNode has fields: Path, TotalCommits
    And AuthorNode has fields: Name, Email
    And CoChangedEntry has fields: FileA, FileB, CoCount, CommitsA, CommitsB, CouplingStrength
    And RenameEntry has fields: OldPath, NewPath, CommitHash

  Scenario: Session types are defined
    Given the domain/graph/session.go file
    Then SessionNode has fields: ID, InstanceID, StartedAt, Source
    And ActionNode has fields: ID, SessionID, Tool, Timestamp, Intent
    And CaptureRequest has fields: SessionID, InstanceID, Source, Tool, Intent
    And CaptureResult has fields: ActionID, FilesModified, FilesCreated
    And CaptureBaseline has fields: SessionID, FileHashes map

  Scenario: Query types are defined
    Given the domain/graph/query.go file
    Then ImpactRequest has fields: FilePath, Depth, TopN, MinCount
    And ImpactResult has fields: Target, CoChanged entries, RenameChain
    And TimelineRequest has fields: SessionID, Since, Until
    And TimelineResult has fields: Sessions list

  Scenario: Repository interface is complete
    Given the domain/graph/repository.go file
    Then GraphRepository interface defines lifecycle methods: Open, Close, InitSchema, Drop
    And write methods: InsertCommit, InsertFileChange, InsertCoChanged, InsertRename
    And read methods: GetIndexState, CoChangedFor, ResolveRenames
    And capture methods: InsertSession, InsertAction, InsertActionModifies, InsertActionProduces
    And baseline methods: GetBaseline, SetBaseline

  Scenario: Git client interface is defined
    Given the domain/graph/git_client.go file
    Then GraphGitClient interface defines: CommitLogDetailed, CurrentHead
    And MergeBaseIsAncestor, HashObject, DiffNameOnly, DiffForFiles

  Scenario: Index types are defined
    Given the domain/graph/index.go file
    Then IndexRequest has fields: RepoPath, ForceReindex
    And IndexResult has fields: CommitsIndexed, IsIncremental

  Scenario: CoChangeHint exists in commit domain
    Given the domain/commit/planner.go file
    Then PlanRequest has field CoChangeHints of type []CoChangeHint
    And CoChangeHint has fields: FileA, FileB, Strength
```

## Files to Modify/Create
- `domain/graph/types.go` -- CommitNode, FileNode, AuthorNode, CoChangedEntry, RenameEntry, CommitInfo, FileChange
- `domain/graph/session.go` -- SessionNode, ActionNode, CaptureRequest, CaptureResult, CaptureBaseline
- `domain/graph/query.go` -- ImpactRequest, ImpactResult, TimelineRequest, TimelineResult
- `domain/graph/repository.go` -- GraphRepository interface
- `domain/graph/git_client.go` -- GraphGitClient interface
- `domain/graph/index.go` -- IndexRequest, IndexResult
- `domain/commit/planner.go` -- CoChangeHint, CoChangeHints field (already present)

## Steps
### Step 1: Define core types in `types.go`
Define CommitNode, FileNode, AuthorNode, CoChangedEntry, RenameEntry. Also CommitInfo and FileChange (used by git client for raw log parsing).

### Step 2: Define session types in `session.go`
SessionNode, ActionNode, CaptureRequest, CaptureResult, CaptureBaseline.

### Step 3: Define query types in `query.go`
ImpactRequest/Result, TimelineRequest/Result.

### Step 4: Define repository interface in `repository.go`
Single GraphRepository interface covering lifecycle, write, read, capture, and baseline operations.

### Step 5: Define git client interface in `git_client.go`
GraphGitClient interface with methods needed by index and capture services.

### Step 6: Define index types in `index.go`
IndexRequest, IndexResult value objects.

### Step 7: Verify CoChangeHint in commit domain
Confirm `domain/commit/planner.go` has CoChangeHint type and CoChangeHints field on PlanRequest.

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
# No external imports in domain/graph
go vet ./domain/graph/...
# Check for import violations
grep -r '"github.com' domain/graph/ | grep -v 'gitagenthq/git-agent/domain' && exit 1 || true
# Build passes
make build
make test
```

## Success Criteria
- All types compile with `go vet ./domain/graph/...`
- Zero external imports in `domain/graph/` (only stdlib and sibling domain packages)
- No imports of `infrastructure/` or `application/` from domain
- `domain/commit/planner.go` contains CoChangeHint and CoChangeHints field
- `make build` succeeds
- `make test` passes
