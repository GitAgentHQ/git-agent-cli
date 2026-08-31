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

  Scenario: Bare git-agent evaluates diff and updates scopes when new modules are detected
    Given a git repository with uncommitted changes and existing scopes
    When git-agent is executed with no subcommands
    Then ScopeService.Generate is called with existing scopes
    And newly detected scopes are appended to project config on disk

  Scenario: Bare git-agent reports clean working tree when no changes exist
    Given a git repository with no changed files
    When git-agent is executed with no subcommands
    Then git-agent outputs clean status without running commit

  Scenario: Bare git-agent keeps LLM heartbeat off stdout
    Given a bare git-agent invocation is waiting for an LLM response
    When the LLM heartbeat interval elapses
    Then heartbeat diagnostics are written to stderr
    And stdout remains reserved for command output

  Scenario: Bare git-agent preserves mandatory ignores when gitignore service is unavailable
    Given a git repository with no .gitignore file
    And the gitignore provider is unavailable
    When git-agent is executed with no subcommands
    Then a .gitignore file is still created
    And mandatory git-agent files are ignored
