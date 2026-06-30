Feature: init --graph builds the full code graph

  `init --graph` is the explicit cold-start entry for the code graph. It builds
  both layers in one shot: the commit-history + co-change index (L2) and the
  Event-Log projections (L3). It is opt-in — the default `init` wizard does not
  build the graph (the first `commit` does, via graph_autobuild).

  Background:
    Given a git repo with committed history and no graph database

  Scenario: init --graph builds both layers
    When I run `git-agent init --graph`
    Then `.git-agent/graph.db` exists
    And `status` shows commit_count > 0
    And `status` shows co_changed_count > 0

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
