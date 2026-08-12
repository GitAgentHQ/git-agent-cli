Feature: Model resolution

  Scenario: Agent-session environment variables never set the generation model
    Given agent-session model environment variables are set (PI_MODEL, CLAUDE_CODE_MODEL, CODEX_MODEL, MODEL)
    And no --model flag and no config file are provided
    When provider config is resolved
    Then the resolved model is empty

  Scenario: The configured model wins over agent-session environment variables
    Given the user config file sets model "gpt-4"
    And agent-session model environment variables are set (PI_MODEL, CLAUDE_CODE_MODEL, CODEX_MODEL, MODEL)
    When provider config is resolved
    Then the resolved model is "gpt-4"
