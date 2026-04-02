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
