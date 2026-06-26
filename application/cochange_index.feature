Feature: Co-change Index Floor

  Co-change coupling is stored at index time and filtered again at query time.
  The two thresholds must agree: the index must persist any pair the query
  flag can ask for, so that lowering `impact --min-count` never silently
  returns nothing for data that was discarded during indexing.

  Background:
    Given a git repository indexed into the graph

  Scenario: A single co-occurrence is pruned as incidental noise
    Given files "a.go" and "c.go" changed together in exactly one commit
    When the co-change index is built
    Then no co_changed row exists for the "a.go"/"c.go" pair

  Scenario: A pair seen twice is persisted as weak signal
    Given files "a.go" and "b.go" changed together in two commits
    When the co-change index is built
    Then a co_changed row exists for the "a.go"/"b.go" pair with coupling_count 2

  Scenario: The query default hides weak pairs
    Given a persisted "a.go"/"b.go" pair with coupling_count 2
    When impact is queried for "a.go" with the default min-count
    Then the pair is not returned

  Scenario: Lowering min-count surfaces the persisted weak pair
    Given a persisted "a.go"/"b.go" pair with coupling_count 2
    When impact is queried for "a.go" with min-count 2
    Then "b.go" is returned
