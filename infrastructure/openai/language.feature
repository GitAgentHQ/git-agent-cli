Feature: Commit message language preference

  Generated commit messages preserve conventional commit type and scope
  formatting while following the configured language preference.

  Scenario: Auto language follows a clear user directive
    Given the language preference is "auto"
    And the primary directive is written in Chinese
    When a commit message is generated
    Then the title, bullets, and explanation are requested in Chinese
    And the conventional commit type and scope syntax remain unchanged

  Scenario: Explicit English preserves hook-compatible case rules
    Given the language preference is "English"
    When a commit message is generated or a multi-file plan is requested
    Then English is requested for the title, bullets, and explanation
    And the English title description is requested in lowercase
    And English bullets are requested with an uppercase first letter

  Scenario Outline: English locale aliases preserve hook-compatible case rules
    Given the language preference is <language>
    When a commit message is generated
    Then English is requested for the title, bullets, and explanation
    And the English title description is requested in lowercase
    And English bullets are requested with an uppercase first letter

    Examples:
      | language |
      | "en-au"  |
      | "en-ca"  |

  Scenario: Explicit language applies to messages, retries, and plans
    Given the language preference is "Japanese"
    When a commit message is generated or a multi-file plan is requested
    Then the title, bullets, and explanation are requested in Japanese
    And hook retries include an explicit Japanese language instruction
    And English-only case rules are not applied

  Scenario: Auto language preserves English case rules when English is selected
    Given the language preference is "auto"
    And no clear language appears in the primary directive
    When a commit message is generated or a multi-file plan is requested
    Then English is requested for the title, bullets, and explanation
    And the English title description is requested in lowercase
    And English bullets are requested with an uppercase first letter

  Scenario: Auto language keeps Chinese natural capitalization
    Given the language preference is "auto"
    And the primary directive is written in Chinese
    When a commit message is generated or a multi-file plan is requested
    Then Chinese is requested for the title, bullets, and explanation
    And English-only case rules are not applied
