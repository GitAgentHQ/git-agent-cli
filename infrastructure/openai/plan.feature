Feature: Commit Planning via LLM

  Plan splits changed files into atomic commit groups. When the project has
  configured scopes, the scoped system prompt requires every group title to
  use one of them. Scope generation deliberately skips documentation and
  asset directories, so a docs-only changeset can leave the LLM with no
  legal scope — it then returns an empty groups array.

  Scenario: Scoped plan returning no groups retries without the scope constraint
    Given a project config whose scopes cover none of the changed files
    And the LLM returns an empty groups array for the scoped plan request
    When Plan is called
    Then the plan request is retried once with the unscoped system prompt
    And the retry user prompt omits the REQUIRED scopes list
    And the plan from the retry is returned without error

  Scenario: Empty plan after the unscoped retry is an error
    Given a project config with scopes
    And the LLM returns an empty groups array for both plan requests
    When Plan is called
    Then an "LLM returned empty plan" error is returned after two requests

  Scenario: Unscoped empty plan fails without a retry
    Given no configured scopes
    And the LLM returns an empty groups array
    When Plan is called
    Then an "LLM returned empty plan" error is returned after one request
