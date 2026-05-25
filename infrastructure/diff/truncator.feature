Feature: Diff Truncation

  The truncator bounds the diff handed to the LLM so the request body stays
  within the provider's limits. Line count is a soft, user-tunable cap; byte
  size is a hard guard against the API request-body limit (large vendored or
  minified files have few lines but many bytes).

  Scenario: Diff within both limits is left unchanged
    Given a diff under the line cap and under the byte cap
    When Truncate is called
    Then the diff is returned unchanged
    And no truncation is reported

  Scenario: Line cap truncates a diff with many short lines
    Given a diff whose line count exceeds maxLines
    And whose byte size is under maxBytes
    When Truncate is called
    Then the content is cut to maxLines lines
    And truncation is reported

  Scenario: Byte cap truncates long lines the line cap cannot catch
    Given a diff whose line count is under maxLines
    But whose byte size exceeds maxBytes
    When Truncate is called
    Then the content is cut to at most maxBytes bytes
    And truncation is reported

  Scenario: A single oversized line is cut on a UTF-8 boundary
    Given a diff that is one line larger than maxBytes
    When Truncate is called
    Then the content is at most maxBytes bytes
    And the content is valid UTF-8

  Scenario: Disabled caps perform no truncation
    Given maxLines is zero and maxBytes is zero
    When Truncate is called
    Then the diff is returned unchanged
