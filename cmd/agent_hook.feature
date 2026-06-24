Feature: Automatic Agent Action Capture (R21)

  To make behavioral traceability real, git-agent wires itself into Claude
  Code so that every Edit/Write/Bash tool call is captured into the graph
  with no manual step. The capture must be fast (<200ms), never call an LLM,
  and never block the agent on failure.

  Background:
    Given a git repository

  Scenario: init installs a PostToolUse hook into .claude/settings.json
    When init runs with the agent-hook action
    Then ".claude/settings.json" contains a PostToolUse hook
    And the hook matcher covers Edit, Write, and Bash
    And the hook command invokes "git-agent capture --source claude-code"

  Scenario: Installing the hook preserves existing settings
    Given ".claude/settings.json" already defines an unrelated permission
    When init runs with the agent-hook action
    Then the unrelated permission is preserved
    And the PostToolUse hook is added alongside it

  Scenario: Installing the hook is idempotent
    Given the agent-hook is already installed
    When init runs with the agent-hook action again
    Then ".claude/settings.json" contains exactly one git-agent PostToolUse hook

  Scenario: capture reads the tool name from the Claude Code hook payload
    Given a Claude Code PostToolUse payload on stdin with tool_name "Edit"
    And no --tool flag is passed
    When capture runs
    Then the recorded action's tool is "Edit"

  Scenario: capture groups actions by the Claude Code session id
    Given a Claude Code PostToolUse payload on stdin with session_id "claude-abc"
    And no --instance-id flag is passed
    When capture runs
    Then the action is recorded under instance "claude-abc"

  Scenario: An explicit flag overrides the stdin payload
    Given a Claude Code PostToolUse payload on stdin with tool_name "Edit"
    And --tool "Write" is passed
    When capture runs
    Then the recorded action's tool is "Write"

  Scenario: capture without stdin still works for manual invocation
    Given stdin is an interactive terminal
    When capture runs with --source human
    Then capture does not block on stdin
