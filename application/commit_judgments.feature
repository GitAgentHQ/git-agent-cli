Feature: System One judgments in the commit flow

  Background:
    Given a repository with changes in two top-level directories
    And a commit service with a conventional hook

  Scenario: The grouping judgment replaces the planner
    Given a group decider that keeps both buckets apart
    When a commit runs in the on mode
    Then two commits are created
    And the planner is not asked for a grouping

  Scenario: The grouping judgment never carries the diff body
    Given a group decider
    When a commit runs
    Then the judgment state holds paths and line counts only

  Scenario: Shadow mode keeps the planner's grouping
    Given a layer in shadow mode
    And a group decider that merges both buckets
    And a planner that keeps them apart
    When a commit runs
    Then the planner's grouping is used
    And the disagreement is recorded as an observation

  Scenario: A confident verdict pins the title prefix
    Given a type and scope judge answering fix(app) with confidence 0.9
    And a seam floor of 0.85
    When a commit runs
    Then the message generator receives the prefix "fix(app)"

  Scenario: A weak verdict leaves the title to the model
    Given a type and scope judge answering fix(app) with confidence 0.6
    And a seam floor of 0.85
    When a commit runs
    Then the message generator receives no prefix

  Scenario: An unmeasured language skips the judge
    Given a type and scope judge
    And a project configured for language zh-CN
    When a commit runs
    Then the judge is not asked
    And the message generator receives no prefix

  Scenario: A wording rejection is rewritten without a re-plan
    Given a failure router answering rewrite
    And a hook that rejects the first message
    When a commit runs
    Then the same group is drafted again
    And the planner is not asked again

  Scenario: A give-up verdict stops the commit
    Given a failure router answering give_up
    And a hook that rejects every message
    When a commit runs
    Then the commit ends with the hook reason

  Scenario: A judgment failure keeps the existing path
    Given every judgment failing
    When a commit runs
    Then the planner groups the files
    And the message generator receives no prefix
    And the commit still lands
