Feature: Conventional Commits validation hook

  The pre-commit hook validates that commit messages conform to
  Conventional Commits 1.0.0 specification plus project rules
  (body bullets, explanation paragraph, line-length limits,
  Co-Authored-By format).

  Background:
    Given the hook receives a JSON payload on stdin
    And the payload contains a "commit_message" field

  # --- passing ---

  Scenario: Valid full message
    Given the commit message is:
      "feat: add user authentication\n\n- add login endpoint\n- add jwt token generation\n\nThis introduces basic authentication support.\n\nCo-Authored-By: Bot <bot@example.com>"
    When the hook runs
    Then it exits with code 0

  Scenario: Scoped fix with full body
    Given the commit message is:
      "fix(auth): handle null token\n\n- return 401 on missing token\n- add test for null case\n\nA null token caused a panic; this makes the handler return 401.\n\nCo-Authored-By: Bot <bot@example.com>"
    When the hook runs
    Then it exits with code 0

  Scenario: Breaking change with bang marker
    Given the commit message is:
      "feat!: drop support for go 1.20\n\n- remove go 1.20 build tag\n- update ci matrix\n\nGo 1.20 is EOL and no longer supported.\n\nCo-Authored-By: Bot <bot@example.com>"
    When the hook runs
    Then it exits with code 0

  Scenario: Breaking change with scope and bang marker
    Given the commit message is:
      "feat(api)!: remove legacy endpoint\n\n- remove /v1/users route\n- update api docs\n\nThe v1 endpoint was deprecated and is now removed.\n\nCo-Authored-By: Bot <bot@example.com>"
    When the hook runs
    Then it exits with code 0

  Scenario: Valid message without Co-Authored-By footer
    Given the commit message is:
      "chore: update dependencies\n\n- bump go-openai to 1.41\n- bump cobra to 1.8\n\nRoutine dependency update to pick up bug fixes."
    When the hook runs
    Then it exits with code 0

  Scenario: Commit message with escaped quotes in description
    Given the commit message is:
      "feat: handle edge cases\n\n- add null check\n\nThis prevents panics on unexpected input."
    When the hook runs
    Then it exits with code 0

  # --- error: header format ---

  Scenario: Missing type prefix
    Given the commit message is "add login feature"
    When the hook runs
    Then it exits with code 1
    And stderr contains "Conventional Commits"

  Scenario: Missing colon and space separator
    Given the commit message is "feat add login"
    When the hook runs
    Then it exits with code 1

  Scenario: Empty description after type
    Given the commit message is "feat:"
    When the hook runs
    Then it exits with code 1

  Scenario: Invalid type not in allowed list
    Given the commit message is "feature: add login"
    When the hook runs
    Then it exits with code 1

  # --- error: description lowercase ---

  Scenario: Uppercase letter in description
    Given the commit message is:
      "feat: Add login endpoint\n\n- add route handler\n\nThis adds the login route."
    When the hook runs
    Then it exits with code 1
    And stderr contains "lowercase"

  # --- error: title length ---

  Scenario: Title exceeds fifty characters
    Given the commit message is:
      "feat: add a very long title that exceeds fifty characters here\n\n- add route\n\nThis adds the route."
    When the hook runs
    Then it exits with code 1
    And stderr contains "50 characters"

  # --- error: trailing period ---

  Scenario: Title ends with a period
    Given the commit message is:
      "feat: add login endpoint.\n\n- add route handler\n\nThis adds the login route."
    When the hook runs
    Then it exits with code 1
    And stderr contains "period"

  # --- error: blank line between header and body ---

  Scenario: Body not separated from header by blank line
    Given the commit message is "feat: add x\nbody"
    When the hook runs
    Then it exits with code 1
    And stderr contains "blank line"

  # --- error: body required ---

  Scenario: Header only with no body
    Given the commit message is "feat: add login endpoint"
    When the hook runs
    Then it exits with code 1
    And stderr contains "body is required"

  # --- error: bullet points required ---

  Scenario: Body with no bullet points
    Given the commit message is:
      "feat: add login endpoint\n\nJust some prose.\n\nMore prose."
    When the hook runs
    Then it exits with code 1
    And stderr contains "bullet point"

  # --- error: body line length ---

  Scenario: Body line exceeds seventy-two characters
    Given the commit message is:
      "feat: add login\n\n- add a route handler for the new login endpoint that is being introduced here\n\nThis adds the route."
    When the hook runs
    Then it exits with code 1
    And stderr contains "72 characters"

  # --- error: explanation paragraph missing ---

  Scenario: Body ends after bullet points with no explanation
    Given the commit message is:
      "feat: add login endpoint\n\n- add route handler\n- add session support"
    When the hook runs
    Then it exits with code 1
    And stderr contains "explanation paragraph"

  # --- error: Co-Authored-By malformed ---

  Scenario: Co-Authored-By missing email angle brackets
    Given the commit message is:
      "feat: add login endpoint\n\n- add route handler\n\nThis adds the route.\n\nCo-Authored-By: Bot bot@example.com"
    When the hook runs
    Then it exits with code 1
    And stderr contains "Co-Authored-By"
