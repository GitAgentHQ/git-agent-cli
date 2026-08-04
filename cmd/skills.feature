Feature: Skills Command Serves Embedded Documentation

  The `skills` command exposes git-agent's own usage documentation from the
  binary. The repository's skill stub (skills/using-git-agent/SKILL.md) is a
  discovery stub; the actual workflows, flags, and command reference live here,
  embedded at build time so the content always matches the installed version.

  Scenario: skills get core prints the main usage guide
    When skills get is run with "core"
    Then the output is the embedded core guide
    And the output contains the heading "# Git Agent CLI"
    And the output mentions "skills get cli" for the full command reference

  Scenario: skills get cli prints the full command reference
    When skills get is run with "cli"
    Then the output is the embedded command reference
    And the output contains the "git-agent commit" command entry

  Scenario: skills get with an unknown document fails with a hint
    When skills get is run with "unknown"
    Then the command exits 1 with a general error
    And the error message lists the available documents

  Scenario: skills get without a name fails
    When skills get is run with no arguments
    Then the command exits 1 with a general error
    And the error message asks for exactly one document name

  Scenario: skills list shows every available document
    When skills list is run
    Then the output contains both "core" and "cli"

  Scenario: skills with no subcommand prints help
    When skills is run with no arguments
    Then the command prints usage and exits 0
