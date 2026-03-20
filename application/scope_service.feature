Feature: Scope Service

  Background:
    Given a git repository with commits and tracked files

  Scenario: Generate scopes from project structure
    When ScopeService.Generate is called with maxCommits=20
    Then the LLM receives commits, dirs, and files as context
    And the returned scopes reflect the top-level directories

  Scenario: Generate fails when LLM is unavailable
    Given the LLM returns an error
    When ScopeService.Generate is called
    Then an error is returned

  Scenario: MergeAndSave creates a new project.yml
    Given no existing project.yml
    When ScopeService.MergeAndSave is called with scopes ["cmd", "app"]
    Then project.yml is created containing "cmd" and "app"

  Scenario: MergeAndSave deduplicates scopes
    Given project.yml contains scopes ["cmd", "app"]
    When ScopeService.MergeAndSave is called with scopes ["app", "infra"]
    Then project.yml contains exactly one "app" entry
    And project.yml contains "cmd" and "infra"

  Scenario: MergeAndSave is case-insensitive for deduplication
    Given project.yml contains scopes ["CMD"]
    When ScopeService.MergeAndSave is called with scopes ["cmd"]
    Then project.yml contains exactly one scope matching "cmd" (case-insensitive)
