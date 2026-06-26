# BDD Specifications: git-agent graph

## Overview

42 scenarios across 6 feature areas covering graph indexing, impact query,
action capture, timeline, diagnose stub, and graph lifecycle management.

P2 scenarios are explicitly tagged and out of scope for v1.

## Testing Strategy

### Unit Tests

- `domain/graph/` -- DTOs are pure value objects, no behavior to test
- `infrastructure/graph/sqlite_repository.go` -- test against real SQLite (embedded, fast)
- `application/graph_index_service.go` -- mock GraphRepository and GitClient
- `application/graph_ensure_index.go` -- mock GraphRepository and GitClient
- `application/graph_impact_service.go` -- mock GraphRepository and EnsureIndexService
- `application/graph_capture_service.go` -- mock GraphRepository and GitClient

### Integration Tests

- Full index -> query cycle on a small real repository
- Incremental index correctness: index, add commits, re-index, verify
- EnsureIndex: verify auto-index on missing DB, unreachable hash, and incremental
- Capture -> timeline cycle: capture actions, query timeline, verify order
- Capture -> commit -> action-to-commit linking
- Commit enhancement: verify CoChangeHints are injected into planner

### E2E Tests

Following the existing pattern in `e2e/`:
- `e2e/graph_test.go` -- build binary, invoke as subprocess
- Test `impact` (with auto-indexing)
- Test `capture`, `timeline` (P1b)
- Test `diagnose` (P2 stub -- just verify "not yet implemented" output)
- No build tags needed -- all graph tests run unconditionally with pure Go SQLite

### Test Data

- Small test repository with 5-10 commits, 3-4 files
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
        When EnsureIndex runs (triggered by a query or commit)
        Then a graph database should be created at ".git-agent/graph.db"
        And the graph should contain 3 Commit nodes
        And the graph should contain 5 File nodes
        And the graph should contain Author nodes for each unique committer
        And the graph should contain MODIFIES edges linking commits to files
        And the graph should contain AUTHORED edges linking authors to commits
        And the IndexState should record the latest commit hash

    Scenario: Incremental index after new commits
        Given the repository has an existing graph database
        And the IndexState records commit "abc1234" as last indexed
        And 2 new commits exist after "abc1234"
        And the new commits modify 3 files
        When EnsureIndex runs
        Then only the 2 new commits should be indexed
        And the graph should contain the previously indexed data unchanged
        And the new Commit nodes and MODIFIES edges should be added
        And the IndexState should be updated to the latest commit hash

    Scenario: Incremental index is idempotent
        Given the repository has an existing graph database
        And the IndexState records the latest commit as last indexed
        When EnsureIndex runs
        Then no new data should be added to the graph

    Scenario: Index handles large repositories gracefully
        Given the repository has 10000 commits modifying 5000 files
        When EnsureIndex runs
        Then the indexing should complete without running out of memory
        And the SQLite page cache should be configured for bulk operations
        And bulk import should be used for the initial load
        And the total indexing time should be under 120 seconds

    Scenario: Index skips binary and vendor files
        Given the repository contains files:
            | path                   | type     |
            | src/main.go            | source   |
            | vendor/lib/dep.go      | vendor   |
            | assets/logo.png        | binary   |
            | node_modules/pkg/x.js  | vendor   |
            | go.sum                 | lockfile |
        When EnsureIndex runs
        Then the graph should contain a File node for "src/main.go"
        And the graph should not contain File nodes for vendor directories
        And the graph should not contain File nodes for binary files
        And the graph should not contain File nodes for lock files

    Scenario: Index detects and records file renames
        Given the repository has a commit that renames "old/path.go" to "new/path.go"
        When EnsureIndex runs
        Then the graph should contain a renames row linking "old/path.go" to "new/path.go"
        And the graph should contain File nodes for both "old/path.go" and "new/path.go"
        And CO_CHANGED edges should reflect the combined history of both paths

    Scenario: Incremental index recovers from force-push
        Given the repository has an existing graph database
        And the IndexState records commit "abc1234" as last indexed
        And the repository history was rewritten (force-push)
        And commit "abc1234" is no longer reachable from HEAD
        When EnsureIndex runs
        Then the graph should detect the unreachable last-indexed commit
        And the graph should fall back to a full re-index
        And the command should log a warning about history rewrite
        And the IndexState should record the latest commit hash

    Scenario: Index computes CO_CHANGED edges
        Given the repository has commits where "a.go" and "b.go" are modified together 5 times
        And "a.go" has been modified 8 times total
        And "b.go" has been modified 6 times total
        When EnsureIndex runs
        Then the graph should contain a CO_CHANGED edge from "a.go" to "b.go"
        And the edge should have coupling_count of 5
        And the edge should have coupling_strength of approximately 0.625
        And pairs with fewer than 3 co-changes should not have CO_CHANGED edges
```

### Feature: EnsureIndex (Auto-Indexing)

```gherkin
Feature: EnsureIndex
    As a coding agent or user
    I want the graph to be automatically indexed before queries
    So that I never need to manually run an index command

    Background:
        Given a git repository at a temporary test directory

    Scenario: EnsureIndex creates DB when missing
        Given no ".git-agent/graph.db" exists
        When I run "git-agent impact src/main.go"
        Then a graph database should be created at ".git-agent/graph.db"
        And the full git history should be indexed
        And the impact query should return results (or empty if no co-changes)
        And the command should exit with code 0

    Scenario: EnsureIndex does incremental update
        Given an existing graph database with 100 commits indexed
        And 5 new commits exist after the last indexed commit
        When I run "git-agent impact src/main.go"
        Then only the 5 new commits should be indexed
        And the impact query should include results from all 105 commits

    Scenario: EnsureIndex re-indexes after force-push
        Given an existing graph database
        And the last indexed commit is no longer reachable from HEAD
        When I run "git-agent impact src/main.go"
        Then the graph should be fully re-indexed
        And a warning about history rewrite should appear on stderr

    Scenario: EnsureIndex failure returns exit code 3
        Given the repository is in a state where indexing fails
        When I run "git-agent impact src/main.go"
        Then the exit code should be 3
        And the error should indicate auto-index failure

    Scenario: EnsureIndex runs before commit flow
        Given an existing graph database
        And staged files exist for commit
        When I run "git-agent commit"
        Then EnsureIndex should run before planning
        And co-change data should be available for the planner
```

### Feature: Impact Query

```gherkin
Feature: Impact Query
    As a coding agent
    I want to query the co-change impact of a file
    So that I can understand what files are affected by a change

    Background:
        Given a git repository with an indexed graph database
        And the following CO_CHANGED relationships exist:
            | file1          | file2          | strength |
            | pkg/service.go | db/store.go    | 0.7      |
            | pkg/utils.go   | pkg/service.go | 0.5      |

    Scenario: Query impact of a single file via co-change
        When I run "git-agent impact pkg/service.go"
        Then the output should list co-changed files:
            | file        | reason    |
            | db/store.go | co-change |
            | pkg/utils.go| co-change |
        And co-change results should include coupling strength

    Scenario: Query returns empty result for isolated file
        Given the file "config/cfg.go" has no CO_CHANGED edges above the threshold
        When I run "git-agent impact config/cfg.go"
        Then the output should indicate no impact detected
        And the exit code should be 0

    Scenario: Agent queries via CLI and gets JSON output
        When I pipe "git-agent impact pkg/service.go"
        Then the output should be valid JSON
        And the JSON should have "target", "co_changed" fields
        And each co_changed entry should have "path", "coupling_count", "coupling_strength"
        And the JSON should include a "query_ms" field

    Scenario: Terminal output is human-readable text
        When I run "git-agent impact pkg/service.go" in a terminal
        Then the output should be human-readable text (not JSON)
        And the text should list co-changed files with strength

    Scenario: --json flag forces JSON in terminal
        When I run "git-agent impact pkg/service.go --json" in a terminal
        Then the output should be valid JSON

    Scenario: --text flag forces text when piped
        When I pipe "git-agent impact pkg/service.go --text"
        Then the output should be human-readable text

    Scenario: Impact resolves file renames
        Given "old/service.go" was renamed to "pkg/service.go" in a previous commit
        And "old/service.go" had CO_CHANGED relationships with "db/store.go"
        When I run "git-agent impact pkg/service.go"
        Then the results should include co-change history from both "old/service.go" and "pkg/service.go"
        And "db/store.go" should appear in the co-changed results

    Scenario: Impact query on non-existent file
        When I run "git-agent impact nonexistent.go"
        Then the command should exit with code 1
        And the error message should indicate the file is not in the graph
```

### Feature: Commit Enhancement

```gherkin
Feature: Commit Enhancement
    As a user
    I want the commit planner to be aware of co-change relationships
    So that commit grouping is improved automatically

    Background:
        Given a git repository with an indexed graph database
        And staged files exist for commit

    Scenario: Co-change hints injected into planner
        Given the graph contains co-change data for staged files
        And "src/service.go" co-changes with "src/service_test.go" at strength 0.8
        When I run "git-agent commit"
        Then the planner should receive CoChangeHints for staged files
        And the hints should include coupling strength

    Scenario: Commit works without graph DB
        Given no ".git-agent/graph.db" exists
        When I run "git-agent commit"
        Then the commit should proceed normally without co-change hints
        And no error should be displayed about the missing graph

    Scenario: Commit works with empty co-change data
        Given an indexed graph database exists
        But no CO_CHANGED edges exist for the staged files
        When I run "git-agent commit"
        Then the commit should proceed normally
        And the planner should receive empty CoChangeHints
```

### Feature: Action Capture

```gherkin
@P1b
Feature: Action Capture
    As a coding agent hook
    I want to record each tool call's diff into the graph
    So that fine-grained action history is available for timeline

    Background:
        Given a git repository with an indexed graph database
        And the working directory has uncommitted changes

    Scenario: Capture an agent edit action with delta tracking
        Given I modified "src/main.go" with an Edit tool
        And the modification adds 3 lines and removes 1 line
        And no prior capture baseline exists for "src/main.go"
        When I run "git-agent capture --source claude-code --tool Edit"
        Then a Session node should exist with source "claude-code"
        And an Action node should be created with tool "Edit"
        And the Action should contain the unified diff for "src/main.go" only
        And an ACTION_MODIFIES edge should link the Action to "src/main.go"
        And the edge should have additions=3 and deletions=1
        And the capture_baseline should store the current hash for "src/main.go"
        And the command should exit with code 0

    Scenario: Delta capture attributes only newly changed files
        Given I previously captured changes to "src/main.go" (baseline exists)
        And "src/main.go" has not changed since the last capture
        And I modified "src/utils.go" with an Edit tool
        When I run "git-agent capture --source claude-code --tool Edit"
        Then the Action should only list "src/utils.go" in files_changed
        And the diff should not contain changes from "src/main.go"
        And the ACTION_MODIFIES edge should only link to "src/utils.go"
        And the capture_baseline should be updated for "src/utils.go"

    Scenario: Capture appends to existing active session
        Given a Session "s1" exists with source "claude-code" started 5 minutes ago
        And "s1" already has 2 actions
        When I run "git-agent capture --source claude-code --tool Write"
        Then the Action should be added to Session "s1" (not a new session)
        And the Action id should be "s1:3"

    Scenario: Capture creates new session after timeout
        Given a Session "s1" exists with source "claude-code" started 45 minutes ago
        When I run "git-agent capture --source claude-code --tool Edit"
        Then a new Session "s2" should be created
        And "s1" should have ended_at set automatically

    Scenario: Capture with no diff is a no-op
        Given the working directory has no uncommitted changes
        When I run "git-agent capture --source claude-code --tool Edit"
        Then no Action node should be created
        And the output should indicate skipped with reason "no changes detected"
        And the command should exit with code 0

    Scenario: Capture truncates large diffs
        Given I modified a file producing a diff larger than 100KB
        When I run "git-agent capture --source claude-code --tool Bash"
        Then the stored diff should be truncated at 100KB
        And the diff should end with "[truncated]"

    Scenario: Capture with custom message
        When I run "git-agent capture --source human --message 'fixed auth bug'"
        Then the Action node should have message "fixed auth bug"

    Scenario: End session explicitly
        Given a Session "s1" exists with source "claude-code"
        When I run "git-agent capture --source claude-code --end-session"
        Then Session "s1" should have ended_at set to now
        And no new Action should be created

    Scenario: Capture skips silently when DB is locked
        Given another process holds a write lock on ".git-agent/graph.db"
        When I run "git-agent capture --source claude-code --tool Edit"
        Then the command should exit with code 0
        And stderr should contain a warning about lock contention
        And no Action should be recorded

    Scenario: Concurrent agents use separate sessions via instance_id
        Given a Session "s1" exists with source "claude-code" and instance_id "1234"
        When I run "git-agent capture --source claude-code --tool Edit --instance-id 5678"
        Then a new Session "s2" should be created with instance_id "5678"
        And "s1" should remain active (different instance)

    Scenario: Capture without prior index creates graph DB
        Given the repository has no existing graph database
        When I run "git-agent capture --source claude-code --tool Edit"
        Then a graph database should be created at ".git-agent/graph.db"
        And the Session and Action nodes should be stored

    Scenario: Claude Code PostToolUse hook triggers capture
        Given a Claude Code PostToolUse hook is configured for "Edit|Write|Bash"
        And the hook command is "git-agent capture --source claude-code --tool $CLAUDE_TOOL_NAME --instance-id $PPID"
        And the agent modifies "src/main.go" via the Edit tool
        When the PostToolUse hook fires
        Then "git-agent capture --source claude-code --tool Edit" should be invoked
        And the command should exit with code 0
        And an Action node should exist with tool "Edit" and source "claude-code"

    Scenario: Actions linked to resulting commit
        Given a Session "s1" exists with source "claude-code"
        And the following actions exist in session "s1":
            | action | tool  | files_changed   |
            | s1:1   | Edit  | src/main.go     |
            | s1:2   | Write | src/main_test.go|
            | s1:3   | Bash  |                 |
        And no action_produces edges exist for these actions
        When I run "git-agent commit" and a commit is created with hash "abc123"
        Then action_produces edges should link "s1:1" and "s1:2" to commit "abc123"
        And action "s1:3" should not be linked (no file overlap with committed files)
```

### Feature: Timeline

```gherkin
@P1b
Feature: Timeline
    As a developer or coding agent
    I want to view a timeline of agent and human actions
    So that I can understand what changes were made and when

    Background:
        Given a git repository with an indexed graph database
        And the following sessions and actions exist:
            | session | source      | started_at           | actions |
            | s1      | claude-code | 2026-04-06T14:00:00Z | 3       |
            | s2      | human       | 2026-04-06T14:20:00Z | 1       |
            | s3      | claude-code | 2026-04-06T15:00:00Z | 5       |

    Scenario: Timeline shows raw actions (offline)
        When I run "git-agent timeline --since 2h"
        Then the output should list all 3 sessions
        And each session should include its actions with diffs
        And no summary field should be populated
        And the command should not make any LLM calls

    Scenario: Timeline filtered by source
        When I run "git-agent timeline --source claude-code"
        Then only sessions s1 and s3 should appear
        And session s2 (human) should not appear

    Scenario: Timeline filtered by file
        Given action s1:2 modified "src/main.go"
        And action s3:1 modified "src/main.go"
        When I run "git-agent timeline --file src/main.go"
        Then only sessions containing actions that touched "src/main.go" should appear
        And within each session, only matching actions should be shown

    @P2
    Scenario: Timeline with compression (requires LLM)
        When I run "git-agent timeline --since 2h --compress"
        Then each session should have a summary field with a human-readable description
        And individual actions should not be listed (only action_count)
        And the LLM should be called with grouped diffs for each session

    @P2
    Scenario: Timeline compression fails gracefully without LLM
        Given no LLM endpoint is configured
        When I run "git-agent timeline --compress"
        Then the command should exit with code 1
        And the error should indicate LLM is required for compression
        And the hint should suggest using timeline without --compress

    Scenario: Timeline with time range
        When I run "git-agent timeline --since 2026-04-06T14:30:00Z"
        Then only session s3 should appear (started after the cutoff)

    Scenario: Empty timeline
        Given no sessions exist in the graph
        When I run "git-agent timeline"
        Then the output should show empty sessions array
        And total_sessions and total_actions should be 0
```

### Feature: Diagnose Stub

```gherkin
@P2
Feature: Diagnose Stub
    As a user
    I want the diagnose command to exist as a placeholder
    So that it is discoverable even before implementation

    Scenario: Diagnose prints not yet implemented
        When I run "git-agent diagnose"
        Then stderr should contain "not yet implemented"
        And the exit code should be 0

    Scenario: Diagnose with arguments prints not yet implemented
        When I run "git-agent diagnose src/main.go"
        Then stderr should contain "not yet implemented"
        And the exit code should be 0
```

### Feature: Graph Lifecycle

```gherkin
Feature: Graph Lifecycle
    As a coding agent
    I want the graph database to be managed automatically
    So that I can focus on queries without manual maintenance

    Scenario: EnsureIndex auto-adds graph.db to gitignore
        Given a git repository with no ".git-agent/.gitignore"
        When EnsureIndex runs (via any query or commit)
        Then ".git-agent/.gitignore" should exist
        And ".git-agent/.gitignore" should contain "graph.db"

    Scenario: Graph commands outside a git repository return error
        Given the current directory is not a git repository
        When I run "git-agent impact src/main.go"
        Then the exit code should be 1
        And the error should indicate "not a git repository"

    Scenario: Manual reset by deleting DB files
        Given an indexed repository
        When the user runs "rm .git-agent/graph.db*"
        And then runs "git-agent impact src/main.go"
        Then EnsureIndex should create a fresh graph database
        And the full history should be re-indexed

    Scenario: Schema migration runs automatically on version mismatch
        Given an indexed repository with schema version 1
        And the current code expects schema version 2
        When EnsureIndex runs
        Then the schema should be migrated to version 2
        And existing data should be preserved
        And the command should log the migration in verbose mode

    Scenario: Concurrent indexing is rejected via SQLite lock
        Given an indexed repository
        And another process holds a write lock on ".git-agent/graph.db"
        When I run "git-agent impact src/main.go"
        Then EnsureIndex should wait for the lock (up to busy_timeout)
        And if the lock persists, exit code should be 3

    Scenario: Corrupted DB recovers after manual delete
        Given an indexed repository
        And the graph database files are corrupted
        When I run "git-agent impact src/main.go"
        Then the exit code should be 3 or 1
        And the error should suggest deleting ".git-agent/graph.db*"
        When the user deletes the DB files and re-runs
        Then a fresh graph database should be created
```
