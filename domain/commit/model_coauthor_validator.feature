Feature: Model Co-Authored-By trailer enforcement

  When require_model_co_author is enabled, every commit must carry at least
  one Co-Authored-By trailer whose email belongs to a known AI-provider
  domain. The default Git Agent attribution trailer
  (Co-Authored-By: Git Agent <noreply@git-agent.dev>) does NOT satisfy this
  rule on its own — only domains in the allow-list count.

  Background:
    Given a raw commit message string
    And an allow-list of model email domains
    When ValidateModelCoAuthor is called
    Then it returns a ValidationResult

  # --- passing ---

  Scenario: Anthropic model trailer present
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Git Agent <noreply@git-agent.dev>
      Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
      """
    And the allow-list is "anthropic.com,openai.com,google.com"
    Then HasErrors returns false

  Scenario: OpenAI model trailer present
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: GPT-5 <noreply@openai.com>
      """
    And the allow-list is "anthropic.com,openai.com,google.com"
    Then HasErrors returns false

  Scenario: Domain match is case-insensitive
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Claude Opus 4.6 <noreply@ANTHROPIC.COM>
      """
    And the allow-list is "anthropic.com"
    Then HasErrors returns false

  Scenario: User-extended domain trailer present
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Acme Bot <bot@acme.ai>
      """
    And the allow-list is "anthropic.com,acme.ai"
    Then HasErrors returns false

  # --- error: missing model trailer ---

  Scenario: Only Git Agent trailer is not sufficient
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Git Agent <noreply@git-agent.dev>
      """
    And the allow-list is "anthropic.com,openai.com,google.com"
    Then HasErrors returns true
    And Errors contains "Co-Authored-By trailer from one of"

  Scenario: No Co-Authored-By trailers at all
    Given the commit message is:
      """
      chore: update dependencies

      - bump cobra from 1.7 to 1.8

      Routine dependency update.
      """
    And the allow-list is "anthropic.com,openai.com,google.com"
    Then HasErrors returns true
    And Errors contains "Co-Authored-By trailer from one of"

  Scenario: Human co-author with non-allow-listed domain is not sufficient
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Alice <alice@example.com>
      """
    And the allow-list is "anthropic.com,openai.com,google.com"
    Then HasErrors returns true

  # --- edge cases ---

  Scenario: Malformed Co-Authored-By line is ignored by domain check
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Bot bot@anthropic.com
      """
    And the allow-list is "anthropic.com"
    Then HasErrors returns true

  Scenario: Empty allow-list rejects any commit
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
      """
    And the allow-list is ""
    Then HasErrors returns true
