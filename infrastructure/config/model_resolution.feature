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

  Scenario: The session model is captured for attribution without setting the generation model
    Given the agent-session model environment variable PI_MODEL is set to "opencode/deepseek-v4-pro"
    And no --model flag and no config file are provided
    When provider config is resolved
    Then the resolved model is empty
    And the resolved session model is "opencode/deepseek-v4-pro"
