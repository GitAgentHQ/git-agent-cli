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

  Scenario: Auto-scope when project.yml is missing
    Given no project.yml exists
    And a ScopeService is configured
    When CommitService.Commit is called with Config=nil
    Then ScopeService.Generate is called
    And the generated scopes are written to .git-agent/project.yml
    And the scopes are used for commit message generation
