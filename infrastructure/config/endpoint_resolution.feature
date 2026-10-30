Feature: Endpoint resolution

  Scenario: There is no built-in endpoint
    Given no --base-url flag, no git config, and no config file set base_url
    When provider config is resolved
    Then the resolved base_url is empty
    And the resolved api_key is empty

  Scenario: The configured endpoint is the only source
    Given the user config file sets base_url "https://api.example.com/v1"
    When provider config is resolved
    Then the resolved base_url is "https://api.example.com/v1"

  Scenario: A missing endpoint reports every remedy
    Given no endpoint is configured
    When the provider configuration is validated
    Then the error names base_url
    And the error names model
    And the error names the user config file

  Scenario: An endpoint without a model reports the missing model
    Given base_url is set
    And no model is set
    When the provider configuration is validated
    Then the error names the missing model