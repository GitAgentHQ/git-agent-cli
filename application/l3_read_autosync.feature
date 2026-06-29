Feature: L3 Read Commands Auto-Sync Projections

  `graph timeline`, `graph provenance`, and `graph diagnose` are read-side
  consumers of the Event-Log projections. Per the event-sourcing/CQRS model,
  projections are refreshed lazily by the next consumer, not eagerly by the
  producer (`capture` only appends). So each L3 read must call `SyncIfStale`
  before reading — a cheap no-op when projections are current, an incremental
  replay when they lag the Event Log — so a `timeline` run reflects a just-
  captured event without a separate manual `graph sync`.

  Background:
    Given a repo with an open graph database and an empty Event Log

  Scenario: A just-captured event is visible without a manual sync
    Given an agent captured a Bash outcome event at seq 1
    But the projections have not been replayed
    When I run `graph timeline`
    Then the timeline shows 1 session with 1 action
    And no `graph sync` was run in between

  Scenario: An empty Event Log does not error
    When I run `graph timeline`
    Then the timeline shows 0 sessions
    And the command exits 0

  Scenario: A projection sync failure does not block the read
    Given the projection sync returns an error
    But the Event Log has one captured event already folded
    When I run `graph timeline`
    Then the command exits 0
    And the timeline shows the previously-folded action
    And the sync error is surfaced only under --verbose

  Scenario: provenance and diagnose also auto-sync
    Given an agent captured a file edit event at seq 1
    But the projections have not been replayed
    When I run `graph provenance <file>`
    Then the provenance shows the captured change
    And no `graph sync` was run in between
