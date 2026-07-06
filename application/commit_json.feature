Feature: Structured commit output for agents
  As an automation agent invoking git-agent commit
  I want the commit result as machine-readable JSON
  So that I can verify the outcome without scraping human text

  Background:
    Given a git repository with changes to commit
    And a configured provider

  Scenario: commit emits a JSON envelope with one entry per commit
    When I run "git-agent commit -o json"
    Then stdout is a single JSON object
    And it has a "commits" array with one object per created commit
    And each commit object carries "title", "message", "files", "sha", and "hook_outcome"
    And the top level carries "dry_run", "committed_count", and "final_sha"
    And "final_sha" equals the "sha" of the last commit object

  Scenario: dry-run reports the plan without committing
    When I run "git-agent commit --dry-run -o json"
    Then "dry_run" is true
    And "committed_count" is 0
    And each commit object has an empty "sha"

  Scenario: a hook-blocked commit fails with a structured error and exit 2
    Given a hook that always rejects the message
    When I run "git-agent commit -o json"
    Then the process exits 2
    And stderr is a JSON object whose "error.code" is 2

  Scenario: hook_outcome reflects whether a hook ran
    Given no commit hook is configured
    When I run "git-agent commit -o json"
    Then each commit object has "hook_outcome" equal to "skipped"
