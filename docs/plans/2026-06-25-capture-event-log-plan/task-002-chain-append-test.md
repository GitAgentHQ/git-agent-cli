# Task 002: RED tests for hash chain append and append-only guard

**type**: test
**depends-on**: ["001"]

## Files
- create: `infrastructure/graph/sha256_hasher_test.go` — canonical-form determinism tests for `SHA256Hasher`
- create: `infrastructure/graph/sqlite_repository_event_test.go` — `AppendEvent`/`HeadHash` chaining tests against a real temp SQLite db
- create: `infrastructure/graph/append_only_guard_test.go` — guard asserting no production code path issues `UPDATE`/`DELETE` on `events`

## BDD Scenario(s)
```gherkin
  Scenario: The first Event chains from Genesis
    Given the Event Log is empty
    When the first capture is appended
    Then the Event seq is 1
    And the Event prev_hash is the Genesis value (64 hex zeros)
    And the Event this_hash covers seq, recorded_at, source, tool_name, and the payload bytes

  Scenario: Ground truth is retained instead of reconstructed
    Given an Edit payload whose tool_input contains old_string and new_string
    When capture is invoked
    Then the Event is stored without invoking git diff or git hash-object on the hot path

  Scenario: Append-only is enforced
    Given the Event Log contains Events
    When any code path attempts an UPDATE or DELETE on the events table
    Then the attempt fails a test guard
    And no production path issues such a statement
```

## What to implement

RED unit tests only. They must compile against the Task 001 contracts but FAIL
because `SHA256Hasher` does not yet exist / `AppendEvent` is the not-implemented
stub. Reference architecture.md "Hash Chain" for chain rules and best-practices.md
§2.2 for single-writer expectations.

### (a) `SHA256Hasher` canonical-form determinism — `sha256_hasher_test.go`

- `TestSHA256Hasher_Deterministic`: hashing the same `(prevHash, EventRecord)` twice
  yields an identical 64-char hex string.
- `TestSHA256Hasher_FieldChangeChangesHash`: mutating any one canonical field
  (`seq`, `recorded_at`, `source`, `instance_id`, `kind`, `tool_name`, `exit_code`,
  or `payload_raw` bytes) produces a different hash; mutating an excluded field
  (e.g. `prev_hash`/`this_hash` storage) does not affect `Hash`'s output for a fixed
  `prevHash`.
- Construct via `NewSHA256Hasher()` (peer of `NewUUIDSessionIDGenerator`) — call
  site does not yet exist, so the test fails to build/run RED.

### (b) `AppendEvent` chaining — `sqlite_repository_event_test.go`

Use a real temp SQLite db (`t.TempDir()` + `NewSQLiteClient` + `InitSchema`),
this is infra-level so the real driver is the unit under test.

- `TestAppendEvent_FirstEventFromGenesis`: append into an empty log → returned
  `EventRecord.Seq == 1` and `PrevHash == strings.Repeat("0", 64)` (Genesis).
- `TestAppendEvent_ChainsPrevToPriorThisHash`: append three events; assert each
  event's `PrevHash` equals the previous event's `ThisHash`, and `Seq` is 1,2,3.
- `TestHeadHash`: after N appends, `HeadHash` returns the last appended `ThisHash`;
  on an empty log returns the Genesis value (or documented empty-head value per
  architecture.md "Hash Chain").

### (c) Append-only guard — `append_only_guard_test.go`

- `TestEventsTableIsAppendOnly_NoProductionMutations`: scan the production Go
  sources under `infrastructure/graph` and `application` (skip `*_test.go`) and
  assert no source issues `UPDATE`/`DELETE`/`INSERT OR REPLACE`/`INSERT OR IGNORE`
  targeting the `events` table (best-practices.md §3 append-only guard, pitfall 12).
  RED until the production code is written such that the guard meaningfully passes;
  for now it fails because the supporting impl/wiring is absent.

## Steps
1. Write `sha256_hasher_test.go` with the two determinism tests referencing `NewSHA256Hasher`.
2. Write `sqlite_repository_event_test.go` with the genesis/chain/head tests using a temp db.
3. Write `append_only_guard_test.go` with the source-scan guard.
4. Run the package tests; confirm each fails for the right reason (missing `SHA256Hasher`, stubbed `AppendEvent`).

## Verification
- `go test ./infrastructure/...` — tests are RED: `sha256_hasher_test.go` fails to build/run because `NewSHA256Hasher` is undefined; chaining tests fail because `AppendEvent` returns the not-implemented stub; the guard fails because the supporting impl is absent. Confirm the failure messages cite these reasons, not unrelated compile errors elsewhere.
