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
