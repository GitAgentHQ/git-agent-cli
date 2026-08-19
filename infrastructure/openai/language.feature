Feature: Commit message language preference

  Generated commit messages preserve conventional commit type and scope
  formatting while following the configured language preference.

  Scenario: Auto language follows a clear user directive
    Given the language preference is "auto"
    And the primary directive is written in Chinese
    When a commit message is generated
    Then the title, bullets, and explanation are requested in Chinese
    And the conventional commit type and scope syntax remain unchanged

  Scenario: Explicit language applies to messages, retries, and plans
    Given the language preference is "Japanese"
    When a commit message is generated or a multi-file plan is requested
    Then the title, bullets, and explanation are requested in Japanese
    And hook retries include an explicit Japanese language instruction

  Scenario: Auto language falls back to English without clear intent language
    Given the language preference is "auto"
    And no clear language appears in the primary directive
    When a commit message is generated
    Then English is requested for the title, bullets, and explanation
