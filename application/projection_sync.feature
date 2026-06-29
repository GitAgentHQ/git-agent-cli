Feature: Projection Sync Idempotency

  `graph sync` brings the derived Projections up to date with the append-only
  Event Log by incrementally replaying only the events past the projected
  high-water mark. The staleness check must read an explicit high-water mark,
  not the count of event_files rows: an Outcome Event (a test run) touches no
  files and so produces no event_files row, so using that row's seq as the mark
  would peg sync below the log tail forever and re-replay — duplicating — the
  tail action on every run.

  Background:
    Given an Event Log whose last event is an Outcome (a "go test" run) that
    touches no files

  Scenario: A sync after a rebuild is a no-op
    Given the Projections have been fully rebuilt from the Event Log
    When the projections are synced
    Then no event is replayed and the action count is unchanged

  Scenario: Repeated syncs never duplicate the tail action
    Given the Projections have been fully rebuilt from the Event Log
    When the projections are synced three times
    Then the action count equals the number of events in the log

  Scenario: A genuinely stale projection still catches up
    Given a new event appended after the last projected event
    When the projections are synced
    Then exactly the new event is replayed and the high-water mark advances to
    the log tail
