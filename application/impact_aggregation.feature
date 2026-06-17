Feature: Feature-level Impact Aggregation

  When an agent is modifying a feature, the feature is a SET of files, not one.
  Impact accepts multiple seed files and aggregates their co-change neighbours,
  so a file coupled to several of the feature's files ranks above one coupled to
  just a single file — surfacing the files most likely to also need changes.

  Background:
    Given a graph with co-change history

  Scenario: A single seed behaves like a plain co-change lookup
    Given "main.go" co-changes with "util.go" at strength 0.8
    When impact is queried for seeds ["main.go"]
    Then "util.go" is returned with seed_matches 1
    And the result targets are ["main.go"]

  Scenario: A file coupled to several seeds ranks above one coupled to a single seed
    Given "auth.go" co-changes with "session.go" at strength 0.5
    And "login.go" co-changes with "session.go" at strength 0.5
    And "auth.go" co-changes with "lonely.go" at strength 0.9
    When impact is queried for seeds ["auth.go", "login.go"]
    Then "session.go" has seed_matches 2 and ranks first
    And "lonely.go" has seed_matches 1 and ranks after it

  Scenario: Aggregated entries report which seeds they relate to
    Given "auth.go" co-changes with "session.go" at strength 0.5
    And "login.go" co-changes with "session.go" at strength 0.4
    When impact is queried for seeds ["auth.go", "login.go"]
    Then "session.go" related_to is ["auth.go", "login.go"]
    And its score is the sum of the two coupling strengths

  Scenario: Seed files never appear as their own results
    Given "a.go" co-changes with "b.go" at strength 0.7
    When impact is queried for seeds ["a.go", "b.go"]
    Then neither "a.go" nor "b.go" appears in the co-changed results
