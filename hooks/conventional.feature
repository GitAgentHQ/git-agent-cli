Feature: Conventional Commits validation hook

  The pre-commit hook validates that commit messages conform to
  Conventional Commits 1.0.0 specification.

  Background:
    Given the hook receives a JSON payload on stdin
    And the payload contains a "commit_message" field

  Scenario: Simple feat type
    Given the commit message is "feat: add login"
    When the hook runs
    Then it exits with code 0

  Scenario: Scoped fix type
    Given the commit message is "fix(parser): handle null"
    When the hook runs
    Then it exits with code 0

  Scenario: Breaking change with bang marker
    Given the commit message is "feat!: drop Node 12"
    When the hook runs
    Then it exits with code 0

  Scenario: Breaking change with scope and bang marker
    Given the commit message is "feat(api)!: remove endpoint"
    When the hook runs
    Then it exits with code 0

  Scenario: Header with body separated by blank line
    Given the commit message is "feat: add x\n\nbody here"
    When the hook runs
    Then it exits with code 0

  Scenario: Body with BREAKING CHANGE footer
    Given the commit message is "fix: x\n\nbody\n\nBREAKING CHANGE: removed"
    When the hook runs
    Then it exits with code 0

  Scenario: Body with BREAKING-CHANGE footer
    Given the commit message is "fix: x\n\nbody\n\nBREAKING-CHANGE: removed"
    When the hook runs
    Then it exits with code 0

  Scenario: Co-Authored-By footer
    Given the commit message is "feat: x\n\nCo-Authored-By: A <a@b>"
    When the hook runs
    Then it exits with code 0

  Scenario: Commit message with escaped quotes in description
    Given the commit message is 'feat: handle "quoted" strings'
    When the hook runs
    Then it exits with code 0

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

  Scenario: Body not separated from header by blank line
    Given the commit message is "feat: add x\nbody"
    When the hook runs
    Then it exits with code 1
    And stderr contains "blank line"

  Scenario: Invalid type not in allowed list
    Given the commit message is "feature: add login"
    When the hook runs
    Then it exits with code 1
