Feature: Related Command Path Normalization

  The graph stores repo-relative paths. The `related` command must accept a
  target path in whatever form a human or agent supplies it and resolve it
  to that canonical form before querying, following git pathspec semantics.

  Background:
    Given a git repository whose root is the current repository
    And a file tracked at repo-relative path "src/auth.go"

  Scenario: A plain repo-relative path is used as-is
    When related is run with "src/auth.go" from the repository root
    Then the target queried is "src/auth.go"

  Scenario: A ./-prefixed path is normalized
    When related is run with "./src/auth.go" from the repository root
    Then the target queried is "src/auth.go"

  Scenario: An absolute path under the repository is made repo-relative
    When related is run with the absolute path to "src/auth.go"
    Then the target queried is "src/auth.go"

  Scenario: A path relative to a subdirectory is resolved against the cwd
    When related is run with "auth.go" from the "src" subdirectory
    Then the target queried is "src/auth.go"

  Scenario: Symlinked roots still match
    Given the caller's path uses a symlink (e.g. /tmp) that git reports resolved (e.g. /private/tmp)
    When related is run with an absolute path under the symlinked root
    Then symlinks on both sides are resolved and the target is repo-relative

  Scenario: A path outside the repository is rejected
    When related is run with a path that resolves outside the repository
    Then the command exits 1 with a general error
    And no query is run against the graph
