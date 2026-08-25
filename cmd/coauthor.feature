Feature: Co-author trailers (--co-author)

  Scenario: git-agent commit --co-author adds a Co-Authored-By trailer
    Given a git repository with staged changes
    When git-agent commit is executed with --co-author "Name <email>"
    Then the committed message contains a Co-Authored-By trailer for each author

  Scenario: Bare git-agent --co-author adds a Co-Authored-By trailer
    Given a git repository with changed files
    When git-agent is executed with no subcommands and --co-author "Name <email>"
    Then the committed message contains a Co-Authored-By trailer for each author

  Scenario: Name-only --co-author survives conventional validation
    Given a git repository with staged changes and conventional hooks enabled
    When git-agent commit is executed with --co-author "OX Alpha"
    Then the committed message contains "Co-Authored-By: OX Alpha"

  Scenario: Direct git-agent run with inference model does not add model Co-Authored-By
    Given a git repository with changed files and an active inference model
    When git-agent is executed directly without an agent session model
    Then the committed message does not contain a model Co-Authored-By trailer

  Scenario: Git-agent run within an agent session infers model Co-Authored-By from session model
    Given a git repository with changed files and an active agent session model
    When git-agent is executed
    Then the committed message contains a Co-Authored-By trailer matching the session model

  Scenario: Unmapped session model still infers Co-Authored-By without provider mapping
    Given a git repository with changed files and an agent session model that maps to no known provider
    When git-agent is executed with require_model_co_author enabled
    Then the commit succeeds
    And the committed message contains a Co-Authored-By trailer titled from the session model under the fallback domain

