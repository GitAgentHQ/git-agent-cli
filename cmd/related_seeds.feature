Feature: Related Seed Resolution

  An agent modifying a feature should be able to ask "what else is likely to
  change?" without enumerating files by hand. The `related` command accepts the
  feature as one or more files, a directory, or — with no arguments — whatever
  is currently changed in the working tree.

  Background:
    Given a git repository indexed into the graph

  Scenario: Multiple file seeds are queried together
    When related runs with two file paths
    Then both are reported as targets
    And a file coupled to both ranks above one coupled to a single seed

  Scenario: A directory seed expands to its tracked files
    When related runs with a directory path
    Then every git-tracked file under that directory becomes a seed

  Scenario: No arguments uses the working-tree changes as seeds
    Given the agent has edited "a.go" and "b.go" but not committed
    When related runs with no arguments
    Then "a.go" and "b.go" are the targets
    And the result lists files that historically change with them

  Scenario: Tooling directories are never used as seeds
    Given the working tree shows changes under ".git-agent/" and ".claude/"
    When related runs with no arguments
    Then no tooling-directory path is used as a seed

  Scenario: A clean working tree with no arguments reports nothing to analyze
    Given the working tree is clean
    When related runs with no arguments
    Then it reports that there are no changes to analyze

  Scenario: --tests narrows the result to related test files
    Given files coupled to the seed include both source and test files
    When related runs with --tests
    Then only the test files (by language-agnostic naming) are reported
