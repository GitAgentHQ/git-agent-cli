Feature: Conventional commit message validation

  The Go-native validator enforces Conventional Commits 1.0.0 plus
  project-specific rules (body bullets, explanation paragraph,
  Co-Authored-By format, line-length limits).

  Background:
    Given a raw commit message string
    When ValidateConventional is called
    Then it returns a ValidationResult

  # --- passing ---

  Scenario: Valid full message
    Given the commit message is:
      """
      feat: add user authentication

      - add login endpoint
      - add jwt token generation

      This introduces basic authentication support.

      Co-Authored-By: Bot <bot@example.com>
      """
    Then HasErrors returns false

  Scenario: Valid full message with scope
    Given the commit message is:
      """
      fix(auth): handle null token

      - return 401 on missing token
      - add unit test for null case

      A null token caused a panic; this makes the handler return 401.

      Co-Authored-By: Bot <bot@example.com>
      """
    Then HasErrors returns false

  Scenario: Valid full message with breaking change bang
    Given the commit message is:
      """
      feat(api)!: remove legacy endpoint

      - remove /v1/users endpoint
      - update client to use /v2/users

      The v1 endpoint was deprecated in 2024 and is now removed.

      Co-Authored-By: Bot <bot@example.com>
      """
    Then HasErrors returns false

  Scenario: Valid message without Co-Authored-By
    Given the commit message is:
      """
      chore: update dependencies

      - bump go-openai from 1.40 to 1.41
      - bump cobra from 1.7 to 1.8

      Routine dependency update to pick up bug fixes.
      """
    Then HasErrors returns false

  # --- error: header format (Rule 1) ---

  Scenario: Missing type prefix
    Given the commit message is:
      """
      add login feature

      - add route handler

      This adds the login route.
      """
    Then HasErrors returns true
    And Errors contains "header must match"

  Scenario: Invalid type not in allowed list
    Given the commit message is:
      """
      feature: add login

      - add route handler

      This adds the login route.
      """
    Then HasErrors returns true

  Scenario: Missing colon-space separator
    Given the commit message is:
      """
      feat add login

      - add route handler

      This adds the login route.
      """
    Then HasErrors returns true

  # --- error: description lowercase (Rule 2) ---

  Scenario: Uppercase letter in description
    Given the commit message is:
      """
      feat: Add login endpoint

      - add route handler
      - add session support

      This adds the login route.
      """
    Then HasErrors returns true
    And Errors contains "lowercase"

  # --- error: title length (Rule 3) ---

  Scenario: Title exceeds fifty characters
    Given the commit message is:
      """
      feat: add a very long title that exceeds fifty characters here

      - add route handler

      This adds the route.
      """
    Then HasErrors returns true
    And Errors contains "50 characters"

  # --- error: trailing period (Rule 4) ---

  Scenario: Title ends with a period
    Given the commit message is:
      """
      feat: add login endpoint.

      - add route handler

      This adds the login route.
      """
    Then HasErrors returns true
    And Errors contains "period"

  # --- error: body required (Rule 6 / missing body) ---

  Scenario: No body at all
    Given the commit message is "feat: add login endpoint"
    Then HasErrors returns true
    And Errors contains "body is required"

  Scenario: Header only with trailing newline
    Given the commit message is "feat: add login\n"
    Then HasErrors returns true
    And Errors contains "body is required"

  # --- error: no bullet points (Rule 6) ---

  Scenario: Body with no bullet points
    Given the commit message is:
      """
      feat: add login endpoint

      Just some prose without bullets.

      More prose here.
      """
    Then HasErrors returns true
    And Errors contains "bullet point"

  # --- error: body line too long (Rule 7) ---

  Scenario: Body line exceeds seventy-two characters
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler for the new login endpoint that is being introduced here

      This adds the login route.
      """
    Then HasErrors returns true
    And Errors contains "72 characters"

  # --- error: explanation paragraph missing (Rule 8) ---

  Scenario: Body ends after bullet points with no explanation
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler
      - add session support
      """
    Then HasErrors returns true
    And Errors contains "explanation paragraph"

  Scenario: Only footer after bullets with no explanation
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      Co-Authored-By: Bot <bot@example.com>
      """
    Then HasErrors returns true
    And Errors contains "explanation paragraph"

  # --- error: Co-Authored-By malformed (Rule 9) ---

  Scenario: Co-Authored-By missing email angle brackets
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Bot bot@example.com
      """
    Then HasErrors returns true
    And Errors contains "Co-Authored-By"

  # --- error: scope not in whitelist (Rule 1b) ---

  Scenario: Scope not in configured whitelist is rejected
    Given the commit message is:
      """
      docs(code-graph-design): restructure docs

      - restructure design docs

      Reorganises the docs.
      """
    And allowed scopes are "app, cli, infra"
    Then HasErrors returns true
    And Errors contains "not in the allowed list"

  Scenario: Scope in configured whitelist passes
    Given the commit message is:
      """
      feat(app): add login endpoint

      - add login endpoint

      This adds the login route.
      """
    And allowed scopes are "app, cli, infra"
    Then HasErrors returns false

  Scenario: Scopeless commit passes when scopes are configured
    Given the commit message is:
      """
      chore: update dependencies

      - bump go-openai from 1.40 to 1.41

      Routine dependency update.
      """
    And allowed scopes are "app, cli, infra"
    Then HasErrors returns false

  Scenario: Any scope passes when no scopes are configured
    Given the commit message is:
      """
      feat(anything): add login

      - add route handler

      This adds the login route.
      """
    And allowed scopes are ""
    Then HasErrors returns false

  # --- warning: past-tense verb in description (W1) ---

  Scenario: Description starts with past-tense verb
    Given the commit message is:
      """
      feat: added user authentication

      - add login endpoint

      This introduces authentication support.
      """
    Then HasErrors returns false
    And Warnings contains "past-tense"

  # --- warning: past-tense verb in bullet (W2) ---

  Scenario: Bullet starts with past-tense verb
    Given the commit message is:
      """
      feat: add user authentication

      - added login endpoint

      This introduces authentication support.
      """
    Then HasErrors returns false
    And Warnings contains "past-tense"
