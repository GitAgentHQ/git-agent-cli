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
        When I run "git-agent graph ownership --file pkg/service.go"
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
        When I run "git-agent graph ownership --path pkg/ --since 90d"
        Then the output should list recent active maintainers for the module
        And "bob@dev.com" should be ranked first for recent activity
        And the output should distinguish between all-time and recent ownership

    Scenario: Query ownership for a directory at module level
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
