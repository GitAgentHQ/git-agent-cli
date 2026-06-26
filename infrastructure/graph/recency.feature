Feature: Recency-weighted Co-change

  Old coupling patterns go stale as a codebase evolves. Co-change strength is
  weighted by an exponential decay of each co-change's age (a one-year
  half-life), so recent couplings dominate the ranking while ancient ones fade.
  The raw occurrence count is preserved for the min-count floor. A backtest
  showed this lifts top-10 recall ~6-9 points on mature repositories and is
  neutral on young ones.

  Background:
    Given a git repository indexed into the graph

  Scenario: A recent coupling outranks an equally-frequent stale one
    Given "A.go" co-changed with "B.go" three times years ago
    And "A.go" co-changed with "C.go" three times recently
    When impact is queried for "A.go"
    Then "C.go" ranks above "B.go"
    And "C.go" has the higher coupling strength

  Scenario: Contemporaneous history reduces to plain count-based strength
    Given all co-changes happened at about the same time
    When co-change is recomputed
    Then strength matches the unweighted count / max-total ratio

  Scenario: Both full and incremental recompute use the same weighting
    Given new commits are indexed incrementally
    Then the touched pairs are re-scored with the same recency decay
