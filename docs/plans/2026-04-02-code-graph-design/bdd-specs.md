# BDD Specifications: git-agent graph

## Overview

30 scenarios across 4 features. Full Gherkin files in [features/](./features/).

## Feature Summary

| Feature | File | Scenarios | Coverage |
|---------|------|-----------|----------|
| Graph Indexing | `graph_indexing.feature` | 10 | Full index, incremental, idempotency, multi-lang, CALLS/IMPORTS extraction, large repos, vendor filtering, CO_CHANGED, symbol rebuild |
| Blast Radius | `blast_radius.feature` | 7 | File-level, symbol-level, depth limiting, isolated file, JSON output, transitive, error |
| Code Ownership | `code_ownership.feature` | 5 | Commit-count, recent maintainers, directory-level, JSON output, single-author |
| Change Patterns | `change_patterns.feature` | 7 | Hotspot ranking, time window, stability, JSON output, limited history, clusters, test exclusion |

## Key Scenarios by Priority

### P0 Scenarios (must pass for v1)

```gherkin
Feature: Graph Indexing

  Scenario: First-time full index of a git repository
    Given a git repository with 3 commits modifying 5 files
    And no existing graph database
    When I run "git-agent graph index"
    Then a graph database should be created at ".git-agent/graph.db"
    And the graph should contain 3 Commit nodes
    And the graph should contain 5 File nodes
    And the graph should contain Author nodes for each unique committer
    And MODIFIES edges should link commits to files
    And AUTHORED edges should link authors to commits
    And the IndexState should record the latest commit hash
    And the exit code should be 0

  Scenario: Incremental index after new commits
    Given an existing graph database indexed up to commit "abc1234"
    And 2 new commits exist after "abc1234" modifying 3 files
    When I run "git-agent graph index"
    Then only the 2 new commits should be processed
    And previously indexed data should remain unchanged
    And the IndexState should update to the latest commit hash

  Scenario: Incremental index is idempotent
    Given an existing graph database indexed up to HEAD
    When I run "git-agent graph index"
    Then no new data should be added
    And stdout should contain "already up to date"

  Scenario: CO_CHANGED edges are computed from commit co-occurrences
    Given a repository where files A and B are modified together in 5 commits
    And file A has 10 total commits and file B has 8 total commits
    When I run "git-agent graph index"
    Then a CO_CHANGED edge should exist between A and B
    And coupling_count should be 5
    And coupling_strength should be 0.5 (5/10)

  Scenario: Commits touching more than 50 files are excluded from CO_CHANGED
    Given a merge commit that touches 120 files
    When I run "git-agent graph index"
    Then no CO_CHANGED edges should be created from that commit

  Scenario: Blast radius for a single file returns co-changed files
    Given an indexed repository
    And file "src/main.go" has CO_CHANGED edges to 3 other files
    When I run "git-agent graph blast-radius src/main.go"
    Then stdout should contain valid JSON
    And the JSON should contain "co_changed" array with 3 entries
    And entries should be sorted by coupling_strength descending
    And the exit code should be 0

  Scenario: Blast radius for isolated file returns empty results
    Given an indexed repository
    And file "README.md" has no CO_CHANGED or IMPORTS edges
    When I run "git-agent graph blast-radius README.md"
    Then the JSON "co_changed" array should be empty
    And the JSON "importers" array should be empty
    And the exit code should be 0

  Scenario: Graph status when no index exists
    Given a git repository with no graph database
    When I run "git-agent graph status"
    Then stdout should contain {"exists": false}
    And stdout should contain a "hint" to run graph index
    And the exit code should be 3

  Scenario: Graph status when index exists
    Given an indexed repository
    When I run "git-agent graph status"
    Then stdout should contain {"exists": true}
    And stdout should contain node_counts and edge_counts
    And the exit code should be 0

  Scenario: Graph reset deletes the database
    Given an indexed repository
    When I run "git-agent graph reset"
    Then ".git-agent/graph.db" should not exist
    And stdout should contain {"deleted": true}

  Scenario: Graph index auto-adds graph.db to gitignore (R09)
    Given a git repository with no ".git-agent/.gitignore"
    When I run "git-agent graph index"
    Then ".git-agent/.gitignore" should exist
    And ".git-agent/.gitignore" should contain "graph.db/"

  Scenario: Graph commands outside a git repository return error
    Given the current directory is not a git repository
    When I run "git-agent graph index"
    Then the exit code should be 1
    And stdout should contain {"error": "not a git repository"}

  Scenario: Force re-index rebuilds the entire graph
    Given an indexed repository
    When I run "git-agent graph index --force"
    Then the graph database should be rebuilt from scratch
    And all nodes and edges should be recreated
    And the IndexState should record the latest commit hash
```

### P1 Scenarios (post-v1)

```gherkin
Feature: Graph Indexing (AST)

  Scenario: Index extracts function symbols from Go files
    Given a Go file "pkg/service.go" with functions "Process", "Transform", "Format"
    When I run "git-agent graph index --ast"
    Then the graph should contain 3 Symbol nodes with kind "function"
    And each Symbol should have file_path, start_line, end_line

  Scenario: Index extracts CALLS edges from function bodies
    Given "Process" calls "Transform" and "Format" in the same file
    When I run "git-agent graph index --ast"
    Then CALLS edges should exist from "Process" to "Transform" and "Format"
    And each CALLS edge should have confidence 1.0

  Scenario: Index extracts IMPORTS edges from import statements
    Given "src/app.ts" imports from "./utils" and "../lib/format"
    And "src/utils.ts" and "lib/format.ts" exist
    When I run "git-agent graph index --ast"
    Then IMPORTS edges should exist from "src/app.ts" to "src/utils.ts" and "lib/format.ts"

  Scenario: Symbol-level blast radius via call chain
    Given an indexed repository with AST data
    And function "CommitService.Commit" is called by "runCommit" in "cmd/commit.go"
    When I run "git-agent graph blast-radius --symbol CommitService.Commit"
    Then the JSON "callers" array should contain "runCommit"
    And each caller entry should include the file path

  Scenario: Hotspots ranked by change frequency
    Given an indexed repository
    When I run "git-agent graph hotspots --top 5"
    Then stdout should contain a "hotspots" array with up to 5 entries
    And entries should be sorted by "changes" descending
    And each entry should include path, changes, authors, last_changed

  Scenario: Ownership by commit count
    Given an indexed repository
    And "src/main.go" has 10 commits by "alice@example.com" and 3 by "bob@example.com"
    When I run "git-agent graph ownership src/main.go"
    Then stdout should list alice first with ratio 0.77
    And bob second with ratio 0.23
```

## Testing Strategy

### Unit Tests

- `domain/graph/` -- DTOs are pure value objects, no behavior to test
- `infrastructure/graph/kuzu_repository.go` -- test against real KuzuDB (embedded, fast)
- `infrastructure/treesitter/` -- test with small code samples per language
- `application/graph_service.go` -- mock GraphRepository and ASTParser

### Integration Tests

- Full index -> query cycle on a small real repository
- Incremental index correctness: index, add commits, re-index, verify

### E2E Tests

Following the existing pattern in `e2e/`:
- `e2e/graph_test.go` -- build binary, invoke as subprocess
- Test `graph index`, `graph blast-radius`, `graph status`, `graph reset`
- All behind `//go:build graph` tag

### Test Data

- Small test repository with 5-10 commits, 3-4 files, 2 languages
- Created in `TestMain` (same pattern as existing e2e tests)
- Deterministic commit hashes via `GIT_COMMITTER_DATE` + `GIT_AUTHOR_DATE`
