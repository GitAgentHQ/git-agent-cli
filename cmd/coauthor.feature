Feature: Co-author trailers (--co-author)

  Scenario: git-agent commit --co-author adds a Co-Authored-By trailer
    Given a git repository with staged changes
    When git-agent commit is executed with --co-author "Name <email>"
    Then the committed message contains a Co-Authored-By trailer for each author

  Scenario: Bare git-agent --co-author adds a Co-Authored-By trailer
    Given a git repository with changed files
    When git-agent is executed with no subcommands and --co-author "Name <email>"
    Then the committed message contains a Co-Authored-By trailer for each author
