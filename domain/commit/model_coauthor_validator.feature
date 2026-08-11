Feature: Model Co-Authored-By trailer enforcement

  When require_model_co_author is enabled, every commit must carry at least
  one Co-Authored-By trailer whose email belongs to a known AI-provider
  domain. The default Git Agent attribution trailer
  (Co-Authored-By: Git Agent <noreply@git-agent.dev>) does NOT satisfy this
  rule on its own — only domains in the allow-list count.

  Built-in allow-list (project.DefaultModelCoAuthorDomains — no
  model_co_author_domains config needed): anthropic.com, openai.com, google.com,
  x.ai, zhipuai.cn, qwen.ai, deepseek.com, moonshot.ai. With
  require_model_co_author on and no model_co_author_domains, the hook merges
  this list in before validating, so a trailer from any of these passes. Custom
  providers still use model_co_author_domains.

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

  # The built-in domains below are the production default allow-list
  # (project.DefaultModelCoAuthorDomains). When require_model_co_author is on
  # and model_co_author_domains is unset/empty, the hook layer merges these in
  # before calling ValidateModelCoAuthor, so a trailer from any of them passes
  # with NO model_co_author_domains config. That merge path is exercised by
  # infrastructure/hook/composite_executor_test.go (RequireModelCoAuthor with an
  # empty ModelCoAuthorDomains); these scenarios pin the list itself.

  Scenario Outline: Built-in provider domain is in the default allow-list
    Given the commit message is:
      """
      feat: add login endpoint

      - add route handler

      This adds the login route.

      Co-Authored-By: <model> <noreply@<domain>>
      """
    And the allow-list is "anthropic.com,openai.com,google.com,x.ai,zhipuai.cn,qwen.ai,deepseek.com,moonshot.ai"
    Then HasErrors returns false

    Examples:
      | model       | domain      |
      | Grok 4.5    | x.ai        |
      | GLM-4.5     | zhipuai.cn  |
      | Qwen3       | qwen.ai     |
      | DeepSeek V3 | deepseek.com |
      | Kimi K2     | moonshot.ai |

  # --- model co-author inference ---

  Scenario Outline: Infer model co-author trailer from model ID
    Given a model ID "<model_id>"
    When InferModelCoAuthor is called
    Then the inferred co-author key is "Co-Authored-By"
    And the inferred co-author value is "<expected_coauthor>"

    Examples:
      | model_id                    | expected_coauthor                          |
      | gemini-3.6-flash-high       | Gemini 3.6 Flash <noreply@google.com>      |
      | opencode/deepseek-v4-pro    | DeepSeek V4 Pro <noreply@deepseek.com>     |
      | claude-3-5-sonnet-20241022  | Claude 3.5 Sonnet <noreply@anthropic.com>  |
      | claude-opus-4-6-thinking    | Claude Opus 4.6 <noreply@anthropic.com>    |
      | gpt-5.6-luna                | GPT 5.6 Luna <noreply@openai.com>          |
      | bailian/qwen3.8-max         | Qwen 3.8 Max <noreply@qwen.ai>             |
      | ark/glm-5-2                 | GLM 5.2 <noreply@zhipuai.cn>               |
      | kimi-k3                     | Kimi K3 <noreply@moonshot.ai>              |
      | grok-4.5                    | Grok 4.5 <noreply@x.ai>                    |
      | grok-4.20-0309-non-reasoning| Grok 4.20 <noreply@x.ai>                   |

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
