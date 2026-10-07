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

  Scenario: The decision layer owns the scope set
    Given a scope decider that creates "app" for application/ and skips .github/
    And the LLM can describe the created scopes
    When ScopeService.Generate is called
    Then only the created scope is returned
    And the LLM is asked only for scope descriptions
    And the LLM is not asked for the scope set

  Scenario: Shadow mode lets the LLM decide and records the difference
    Given a scope decider that creates "cli" for cmd/ with confidence 0.95
    And the LLM derives "domain"
    When ScopeService.Generate is called in shadow mode
    Then the LLM scope "domain" is returned
    And the disagreement is recorded as an observation

  Scenario: The off mode runs no decision
    Given a scope decider that would fail if it ran
    And the LLM derives "app"
    When ScopeService.Generate is called in the off mode
    Then the LLM scope "app" is returned
    And the decider was not asked

  Scenario: A spent call budget skips the decision
    Given a layer whose per-run budget is spent
    And a scope decider that would fail if it ran
    When ScopeService.Generate is called
    Then the LLM decides the scope set
    And the decider was not asked

  Scenario: A decision layer failure falls back to the LLM
    Given a scope decider that returns an error
    And the LLM derives the scopes
    When ScopeService.Generate is called
    Then the LLM scopes are returned without an error
    And the fallback is reported on the warning writer

  Scenario: A missing description does not drop the scope
    Given a scope decider that creates "cli" for cmd/
    And the LLM returns an error for scope descriptions
    When ScopeService.Generate is called
    Then the scope "cli" is returned with no description

  Scenario: A created scope named after a commit type is dropped
    Given a scope decider that creates "docs" for docs/
    When ScopeService.Generate is called
    Then the scope "docs" is not returned

  Scenario: Generate succeeds with empty scopes for a fresh repository
    Given the repository has no commit history or tracked files
    And the LLM derives no scopes
    When ScopeService.Generate is called
    Then no error is returned
    And an empty scope list is returned

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
