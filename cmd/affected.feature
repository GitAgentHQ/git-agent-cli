Feature: Affected Command Path Validation

  The affected command traces test files impacted by changes to the given
  files. A path argument that resolves outside the repository is not a real
  change target — it has no graph data and never will — so the command must
  fail fast with exit 1 instead of silently returning an empty result that
  is indistinguishable from a legitimately empty answer.

  Background:
    Given a git repository indexed into the graph

  Scenario: A path outside the repository is rejected
    When affected is run with a path that resolves outside the repository
    Then the command exits 1 with a general error
    And no query is run against the graph

  Scenario: A nonexistent file under the repository is accepted
    Given a file "src/deleted.go" was tracked and has since been removed
    When affected is run with "src/deleted.go"
    Then the command queries the graph for "src/deleted.go"
