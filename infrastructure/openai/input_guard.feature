Feature: LLM input-size guard

  The CLI must never hand the provider a request that is doomed to be rejected
  for an oversized input, and when the provider does reject one (models with
  smaller context windows, or unbounded prompts such as scope generation from a
  large commit history), the failure must explain how to fix it instead of
  echoing a raw 400.

  Scenario: Oversized input fails fast without an HTTP request
    Given a prompt whose estimated input exceeds the 1M-token ceiling
    When the LLM is called
    Then the call fails immediately with an actionable error
    And no request reaches the API server

  Scenario: Provider token-limit 400 is classified with guidance
    Given the API responds 400 with "The input token count exceeds the maximum number of tokens allowed 1048576."
    When the error is classified
    Then the failure is returned as an APIError with guidance on reducing the input
