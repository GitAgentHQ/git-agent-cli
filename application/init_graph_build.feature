Feature: init --graph builds the full three-layer code graph

  `init --graph` is the explicit cold-start entry for the code graph. Unlike
  `graph index` (which only replayed the Event Log + AST), it builds ALL three
  layers in one shot: the commit-history + co-change index (L2), the Event-Log
  projections (L3), and the AST symbol/call-graph index (L1). It is opt-in —
  the default `init` wizard does not build the graph (the first `commit` does,
  via graph_autobuild).

  Background:
    Given a git repo with committed history and no graph database

  Scenario: init --graph builds all three layers
    When I run `git-agent init --graph`
    Then `.git-agent/graph.db` exists
    And `graph status` shows commit_count > 0
    And `graph status` shows co_changed_count > 0
    And the AST index has nodes > 0

  Scenario: --graph composes with other init steps
    When I run `git-agent init --scope --graph`
    Then scopes are written to config
    And `.git-agent/graph.db` exists

  Scenario: the default wizard does not build the graph (opt-in)
    When I run `git-agent init` with the full wizard
    Then `.git-agent/graph.db` is not created by init
    And the graph is left to be built by the first commit

  Scenario: --graph requires no LLM provider
    When I run `git-agent init --graph` with no API key configured
    Then the command succeeds and builds the graph
