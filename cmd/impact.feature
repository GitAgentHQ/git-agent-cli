Feature: Impact Command Path Normalization

  The graph stores repo-relative paths. The impact command must accept a
  target path in whatever form a human or agent supplies it and resolve it
  to that canonical form before querying, following git pathspec semantics.

  Background:
    Given a git repository whose root is the current repository
    And a file tracked at repo-relative path "src/auth.go"

  Scenario: A plain repo-relative path is used as-is
    When impact is run with "src/auth.go" from the repository root
    Then the target queried is "src/auth.go"

  Scenario: A ./-prefixed path is normalized
    When impact is run with "./src/auth.go" from the repository root
    Then the target queried is "src/auth.go"

  Scenario: An absolute path under the repository is made repo-relative
    When impact is run with the absolute path to "src/auth.go"
    Then the target queried is "src/auth.go"

  Scenario: A path relative to a subdirectory is resolved against the cwd
    When impact is run with "auth.go" from the "src" subdirectory
    Then the target queried is "src/auth.go"

  Scenario: Symlinked roots still match
    Given the caller's path uses a symlink (e.g. /tmp) that git reports resolved (e.g. /private/tmp)
    When impact is run with an absolute path under the symlinked root
    Then symlinks on both sides are resolved and the target is repo-relative

  Scenario: A path outside the repository is left untouched
    When impact is run with a path that resolves outside the repository
    Then the cleaned path is queried unchanged
