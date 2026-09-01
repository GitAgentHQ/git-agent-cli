Feature: Remove historical co-change analysis

  The commit workflow must make decisions from the current changes alone.
  Historical co-change analysis is no longer part of the product.

  Scenario: Historical-analysis commands are unavailable
    Given a git repository
    When git-agent is run with the "related" or "status" command
    Then it reports the command as unknown

  Scenario: A commit does not create a graph database
    Given a git repository with a staged change
    When git-agent creates a commit
    Then no .git-agent/graph.db file is created
