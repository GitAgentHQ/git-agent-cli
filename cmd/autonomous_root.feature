Feature: Autonomous Root Command (bare git-agent)

  Scenario: Bare git-agent initializes scopes when config is missing scopes
    Given a git repository with uncommitted changes
    And project config has no scopes
    When git-agent is executed with no subcommands
    Then ScopeService.Generate is called
    And scopes are saved to project config on disk
    And commit workflow is executed

  Scenario: Bare git-agent generates .gitignore when .gitignore is missing
    Given a git repository with no .gitignore file
    When git-agent is executed with no subcommands
    Then GitignoreService.Generate is called
    And .gitignore is written to repository root

  Scenario: Bare git-agent queries co-change related files for modified files
    Given a git repository with modified files
    When git-agent is executed with no subcommands
    Then co-change provider is queried for related files
    And related context is output

  Scenario: Bare git-agent reports clean working tree when no changes exist
    Given a git repository with no changed files
    When git-agent is executed with no subcommands
    Then git-agent outputs clean status without running commit
