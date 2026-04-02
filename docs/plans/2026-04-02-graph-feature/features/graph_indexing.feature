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

    Scenario: Index rebuilds symbols when file content changes
        Given the repository has an existing graph database
        And "src/main.go" was previously indexed with function "OldFunc"
        And a new commit renames "OldFunc" to "NewFunc" in "src/main.go"
        When I run "git-agent graph index"
        Then the graph should not contain a Symbol node for "OldFunc"
        And the graph should contain a Symbol node for "NewFunc"
        And CALLS edges referencing "OldFunc" should be removed
        And new CALLS edges for "NewFunc" should be created
