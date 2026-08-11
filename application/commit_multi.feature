Feature: Multi-Commit Service

  Background:
    Given a git repository
    And an AI commit message generator
    And an AI commit planner

  Scenario: Staged only produces a single commit (backward compatible)
    Given files are staged
    And no unstaged changes
    When CommitService.Commit is called
    Then the planner receives the staged diff
    And exactly one commit is created

  Scenario: Unstaged only - LLM splits into atomic commits
    Given no staged changes
    And multiple unstaged files with logically distinct changes
    When CommitService.Commit is called
    Then the planner splits files into multiple groups
    And each group is committed separately

  Scenario: Staged and unstaged - staged files are group 0
    Given files are staged
    And additional unstaged changes
    When CommitService.Commit is called
    Then the planner receives both staged and unstaged diffs
    And the staged files form the first commit group

  Scenario: Hook failure after 3 retries triggers re-plan
    Given a hook that always blocks
    And a planner that produces 2 groups
    When CommitService.Commit is called with a hook path
    Then each group is retried 3 times
    And after 3 failures a re-plan is triggered
    And after maxRePlans the commit is aborted with ErrHookBlocked

  Scenario: Dry-run shows all planned commits without executing
    Given multiple file groups planned
    When CommitService.Commit is called with DryRun=true
    Then git.Commit is never called
    And the result contains one SingleCommitResult per planned group
    And result.DryRun is true

  Scenario: A file move is committed atomically, not split in two
    Given a file was moved from an old path to a new path
    And the planner puts the deletion and the addition in separate groups
    When CommitService.Commit is called
    Then both paths are placed in the same commit group
    And exactly one commit records the move as a rename

  Scenario: Auto-scope when project config has no scopes
    Given project config has no scopes
    And a ScopeService is configured
    When CommitService.Commit is called
    Then ScopeService.Generate is called
    And the generated scopes are used in memory without writing to disk

  Scenario: Auto-gitignore is not executed during explicit commit
    Given no .gitignore file exists in the repository
    When CommitService.Commit is called
    Then no .gitignore file is written to the repository root
