Feature: Structural Impact via --symbol flag

  When an agent wants to understand what code a function touches, the impact
  command can accept a symbol name instead of file paths, and traverse the AST
  call/reference graph to find structurally impacted symbols.

  Background:
    Given a repository with AST-indexed Go source

  Scenario: Impact by symbol name
    When "git-agent graph impact --symbol runHandler" is run
    Then the output shows symbols that call or reference runHandler
    And each result shows the symbol name, file path, and kind

  Scenario: Impact by symbol name with --depth
    When "git-agent graph impact --symbol processData --depth 2" is run
    Then the output includes both direct callers and their callers (transitive)

  Scenario: Combined mode merges co-change and structural
    When "git-agent graph impact --symbol runHandler --mode combined" is run
    Then co-changed files and structurally impacted symbols are both shown

  Scenario: Symbol not found
    When "git-agent graph impact --symbol nonexistentFunc" is run
    Then an error message indicates no matching symbol was found

  Scenario: Structural mode without AST index auto-indexes
    Given a repository without AST data
    When "git-agent graph impact --symbol foo --mode structural" is run
    Then the AST index is built automatically before querying

  Scenario: Default mode is cochange
    When "git-agent graph impact main.go" is run without --symbol
    Then the existing co-change behavior is unchanged
