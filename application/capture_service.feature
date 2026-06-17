Feature: Capture Service

  As a coding agent recording fine-grained actions into the graph,
  each capture must reflect only the files the agent actually changed,
  never agent-tooling state (git-agent's graph DB or Claude Code's config).

  Background:
    Given a git repository with a graph database under .git-agent/

  Scenario: A capture excludes agent-tooling directories
    Given the working tree shows ".git-agent/graph.db", ".git-agent/graph.db-shm",
      ".git-agent/graph.db-wal", ".claude/settings.json", and "src/main.go" as changed
    When Capture is called
    Then the recorded action's files_changed is exactly ["src/main.go"]
    And no path under ".git-agent/" or ".claude/" is recorded as agent work

  Scenario: A capture of only tooling metadata is a no-op
    Given the only changed paths are under ".git-agent/" or ".claude/"
    When Capture is called
    Then the capture is skipped
    And no session or action is created

  Scenario: Delta capture records only files whose content changed
    Given "a.go" was captured previously at hash "v1"
    And the working tree now shows "a.go" still at "v1" and a new "b.go"
    When Capture is called
    Then the recorded action's files_changed is exactly ["b.go"]
