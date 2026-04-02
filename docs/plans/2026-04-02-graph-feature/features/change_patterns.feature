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
