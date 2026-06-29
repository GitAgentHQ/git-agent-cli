Feature: Commit Generates the Graph (git-first)

  `git-agent commit` is the primary write path for the code graph, not just a
  reader of it. Each commit grows git history (the co-change substrate) and links
  the agent actions that produced it, so the graph is generated as a byproduct of
  the operations a user already runs — never requiring a separate manual
  `graph index`. This reverses the earlier "commit never forces indexing" stance.

  Generation is strictly best-effort: it runs AFTER the commit is created and a
  failure must never block, fail, or slow the commit itself. It is on by default
  and can be disabled with `graph_autobuild=false` for users who only want commit
  messages and no local graph.

  Scenario: First commit bootstraps the graph from history
    Given a repository with commit history and no .git-agent/graph.db
    And graph_autobuild is not disabled
    When a commit is created
    Then the graph database is created
    And the co-change index reflects the repository history including the new commit
    And action-to-commit provenance is linked for any captured actions

  Scenario: Subsequent commit updates the graph incrementally
    Given a repository whose graph is already built up to the previous commit
    When a new commit is created
    Then only the new commit is folded into the co-change index
    And a full re-index is not performed

  Scenario: A graph failure never blocks the commit
    Given a repository where the graph database cannot be written
    When a commit is created
    Then the commit succeeds and its hash is returned
    And no graph error surfaces to the user except under --verbose

  Scenario: Opt-out leaves the graph untouched
    Given graph_autobuild is set to false
    And a repository with no .git-agent/graph.db
    When a commit is created
    Then the commit succeeds
    And no .git-agent/graph.db is created
