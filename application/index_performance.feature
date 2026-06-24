Feature: Fast, Atomic History Indexing

  Indexing the first time on a large repository must be fast enough not to stall
  an agent, and must leave the graph consistent even if it is interrupted. The
  per-commit writes are staged in one transaction; the co-change recompute runs
  with table statistics so the pure-Go SQLite planner uses its indexes.

  Background:
    Given a git repository

  Scenario: A full index stages all writes in a single transaction
    When the history is indexed
    Then all commit, file, and modifies rows are committed together
    And an error mid-index leaves no partially-written commits

  Scenario: The co-change recompute analyzes before the self-join
    Given a large modifies table
    When co-change is recomputed
    Then ANALYZE has populated table statistics first
    And the self-join uses the modifies indexes rather than a full scan

  Scenario: A single capture still autocommits outside any batch
    Given no index batch is open
    When one row is upserted
    Then it is committed immediately
