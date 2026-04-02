# BDD Specifications: git-agent graph

## Overview

39 scenarios across 5 feature areas covering graph indexing, blast radius,
code ownership, change patterns, and graph lifecycle management.

P2 scenarios are explicitly tagged and out of scope for v1.

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

## BDD Scenarios

### Feature: Graph Indexing

```gherkin
Feature: Graph Indexing
    As a coding agent
    I want to index a git repository into a graph database
    So that I can query code relationships and change patterns

    Background:
        Given a git repository at a temporary test directory
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
            | path                   | type     |
            | src/main.go            | source   |
            | vendor/lib/dep.go      | vendor   |
            | assets/logo.png        | binary   |
            | node_modules/pkg/x.js  | vendor   |
            | go.sum                 | lockfile |
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

    Scenario: Index limits history depth with --max-commits
        Given the repository has 500 commits
        When I run "git-agent graph index --max-commits 100"
        Then only the most recent 100 commits should be indexed
        And the graph should contain at most 100 Commit nodes
        And the command should exit with code 0

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
            | File             | Symbols                  |
            | api/handler.go   | HandleRequest, Validate  |
            | pkg/service.go   | Process, Transform       |
            | pkg/utils.go     | Format, Sanitize         |
            | db/store.go      | Save, Load               |
            | config/cfg.go    | ReadConfig               |
        And the following CALLS relationships exist:
            | caller        | callee    |
            | HandleRequest | Process   |
            | HandleRequest | Validate  |
            | Process       | Transform |
            | Process       | Save      |
            | Transform     | Format    |
            | Transform     | Sanitize  |
        And the following CO_CHANGED relationships exist:
            | file1          | file2          | strength |
            | pkg/service.go | db/store.go    | 0.7      |
            | pkg/utils.go   | pkg/service.go | 0.5      |

    Scenario: Query blast radius of a single file via co-change and call chain
        When I run "git-agent graph blast-radius pkg/service.go"
        Then the output should list affected files:
            | file           | reason          | depth |
            | db/store.go    | co-change       | 1     |
            | pkg/utils.go   | co-change       | 1     |
            | api/handler.go | call-dependency | 1     |
        And each result should include the reason for impact
        And co-change results should include coupling strength

    Scenario: Query blast radius of a specific function
        When I run "git-agent graph blast-radius --symbol Transform pkg/service.go"
        Then the output should list affected symbols by call chain depth:
            | symbol   | file         | depth |
            | Format   | pkg/utils.go | 1     |
            | Sanitize | pkg/utils.go | 1     |
        And upstream callers should also be listed:
            | symbol        | file           | depth |
            | Process       | pkg/service.go | 1     |
            | HandleRequest | api/handler.go | 2     |

    Scenario: Query blast radius with depth limit
        When I run "git-agent graph blast-radius --symbol HandleRequest api/handler.go --depth 1"
        Then the output should only include symbols at depth 1:
            | symbol   | file           |
            | Process  | pkg/service.go |
            | Validate | api/handler.go |
        And symbols beyond depth 1 should not appear in the results

    Scenario: Query returns empty result for isolated file
        Given the file "config/cfg.go" has no CALLS edges to other files
        And "config/cfg.go" has no CO_CHANGED edges above the threshold
        When I run "git-agent graph blast-radius config/cfg.go"
        Then the output should indicate no blast radius detected
        And the exit code should be 0

    Scenario: Agent queries via CLI and gets JSON output
        When I run "git-agent graph blast-radius pkg/service.go"
        Then the output should be valid JSON
        And the JSON should have "target", "target_type", "co_changed", "importers", "callers" fields
        And each co_changed entry should have "path", "coupling_count", "coupling_strength"
        And the JSON should include a "query_ms" field

    Scenario: Blast radius includes transitive co-changes
        Given "a.go" co-changes with "b.go" at strength 0.8
        And "b.go" co-changes with "c.go" at strength 0.6
        When I run "git-agent graph blast-radius a.go --depth 2"
        Then "b.go" should appear at depth 1
        And "c.go" should appear at depth 2
        And deeper transitive co-changes should not appear

    Scenario: Blast radius query on non-existent file
        When I run "git-agent graph blast-radius nonexistent.go"
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
            | author        | file           | commits |
            | alice@dev.com | pkg/service.go | 15      |
            | bob@dev.com   | pkg/service.go | 8       |
            | carol@dev.com | pkg/service.go | 3       |
            | alice@dev.com | pkg/utils.go   | 2       |
            | bob@dev.com   | pkg/utils.go   | 20      |
            | carol@dev.com | db/store.go    | 25      |

    Scenario: Query who owns a file by commit count
        When I run "git-agent graph ownership pkg/service.go"
        Then the output should list authors ordered by commit count:
            | author        | commits | percentage |
            | alice@dev.com | 15      | 57.7%      |
            | bob@dev.com   | 8       | 30.8%      |
            | carol@dev.com | 3       | 11.5%      |
        And the primary owner should be "alice@dev.com"

    Scenario: Query recent maintainers of a module
        Given the following recent commit history in the last 90 days:
            | author        | file           | commits |
            | bob@dev.com   | pkg/service.go | 6       |
            | alice@dev.com | pkg/service.go | 1       |
        When I run "git-agent graph ownership pkg/ --since 90d"
        Then the output should list recent active maintainers for the module
        And "bob@dev.com" should be ranked first for recent activity
        And the output should distinguish between all-time and recent ownership

    Scenario: Query ownership for a directory at module level
        When I run "git-agent graph ownership pkg/"
        Then the output should aggregate ownership across all files in "pkg/"
        And the output should list the top contributors to the module
        And each contributor should show their file-level breakdown

    Scenario: Query ownership with JSON output
        When I run "git-agent graph ownership pkg/service.go --format json"
        Then the output should be valid JSON
        And each entry should have fields: "email", "name", "commits", "percentage", "last_active"

    Scenario: Query ownership for file with single author
        Given "solo.go" has only been modified by "alice@dev.com"
        When I run "git-agent graph ownership solo.go"
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

    Scenario: Query change frequency hotspots
        When I run "git-agent graph hotspots"
        Then the output should list files ordered by change frequency:
            | file           | changes | last_changed |
            | pkg/service.go | 45      | 2026-03-28   |
            | api/handler.go | 38      | 2026-04-01   |
            | pkg/utils.go   | 12      | 2026-03-15   |
        And the output should highlight the top 10 hotspots by default
        And each file should show its total change count and last modification date

    Scenario: Query hotspots with time window
        When I run "git-agent graph hotspots --since 2026-03-03"
        Then only changes from the last 30 days should be counted
        And files unchanged in that period should not appear
        And the output should indicate the time window used

    @P2
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

    @P2
    Scenario: Query stability for a single file
        When I run "git-agent graph stability pkg/service.go"
        Then the output should include file-specific metrics:
            | metric              | value     |
            | total_changes       | 45        |
            | unique_contributors | 3         |
            | avg_change_size     | 15 lines  |
            | last_30d_changes    | 8         |
            | co_changed_files    | 3         |
            | primary_owner       | alice@dev |

    Scenario: Query change patterns with JSON output
        When I run "git-agent graph hotspots --format json --top 5"
        Then the output should be valid JSON
        And the JSON should contain at most 5 entries
        And each entry should have fields: "path", "changes", "last_changed", "contributors"

    Scenario: Query hotspots in repository with limited history
        Given the repository has only 1 commit
        When I run "git-agent graph hotspots"
        Then all files should show a change count of 1
        And the output should note the limited history

    @P2
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
        Then files matching "*_test.go" and "*.test.ts" and "test_*.py" should be excluded
        And files matching "*.generated.go" and "*.pb.go" should be excluded
        And only production source files should appear in results
```

### Feature: Graph Lifecycle

```gherkin
Feature: Graph Lifecycle
    As a coding agent
    I want to manage the graph database lifecycle
    So that I can check status, reset, and handle errors

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

    Scenario: Graph index auto-adds graph.db to gitignore
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

    Scenario: Graph reset recovers from corrupted database
        Given an indexed repository
        And the graph database files are corrupted
        When I run "git-agent graph blast-radius src/main.go"
        Then the exit code should be 1
        And the error should suggest running "git-agent graph reset"
        When I run "git-agent graph reset"
        Then ".git-agent/graph.db" should not exist
        And stdout should contain {"deleted": true}
        When I run "git-agent graph index"
        Then a fresh graph database should be created

    Scenario: Concurrent indexing is rejected via file lock
        Given an indexed repository
        And another process holds ".git-agent/graph.lock"
        When I run "git-agent graph index"
        Then the exit code should be 1
        And stdout should contain {"error": "graph is being indexed by another process"}
```
