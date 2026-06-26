# BDD Specifications — Capture Event Log

Gherkin scenarios for the payload-observed, tamper-evident capture redesign. These
belong to the design phase; implementation translates them into `.feature` files
and RED tests (each observed failing for the right reason before any production
code — the BDD Iron Law).

Canonical vocabulary per `_index.md` Glossary: **Event**, **Event Log**, **Chain**,
**`this_hash`**/**`prev_hash`**, **`seq`**, **Genesis**, **Outcome Event**,
**Out-of-Band Event** (`source=unknown`), **Projection**, **Rebuild**/**Replay**,
**Redaction Digest**, **Typed Placeholder**, **File Blob Ref**, **`VerifyResult`**,
**`ChainBreak`**, **Suspect Window**, **Candidate**.

```gherkin
Feature: Payload-Observed Capture and Tamper-Evident Event Log

  Background:
    Given a git repository with a graph database under .git-agent/
    And the Event Log is empty at Genesis state

  # ---------- Observation (FR1, FR8) ----------

  Scenario: Capture records the observed tool payload verbatim
    Given a Claude PostToolUse payload with tool_name "Edit" editing "src/main.go"
    When capture is invoked with the payload on stdin
    Then a new Event is appended with source "claude-code", kind "tool", tool_name "Edit"
    And the Event payload_raw matches the post-redaction bytes received
    And the Event prev_hash equals the prior chain head this_hash
    And the hook exits 0 within the capture budget

  Scenario: The first Event chains from Genesis
    Given the Event Log is empty
    When the first capture is appended
    Then the Event seq is 1
    And the Event prev_hash is the Genesis value (64 hex zeros)
    And the Event this_hash covers seq, recorded_at, source, tool_name, and the payload bytes

  Scenario: Edit-then-revert is preserved as two Events
    Given an Edit payload that changes "a.go" from X to Y
    And a following Edit payload that changes "a.go" from Y back to X
    When capture is invoked for each payload
    Then the Event Log contains two Events
    And neither capture is reported as skipped

  Scenario: Ground truth is retained instead of reconstructed
    Given an Edit payload whose tool_input contains old_string and new_string
    When capture is invoked
    Then the Event is stored without invoking git diff or git hash-object on the hot path

  # ---------- Tamper-evidence (FR2, FR3, FR4) ----------

  Scenario: graph verify passes on an untouched chain
    Given three chained Events exist
    When "graph verify" runs
    Then it reports status "ok"
    And it exits 0

  Scenario: graph verify detects an edited Event
    Given three chained Events exist
    And the payload_raw of the second Event is edited directly in graph.db without re-chaining
    When "graph verify" runs
    Then it reports status "broken"
    And the first ChainBreak has seq 2 and kind "ROW_EDITED"
    And it exits 4

  Scenario: graph verify detects a deleted Event via sequence gap
    Given five chained Events with seq 1..5
    And the Event at seq 3 is deleted from graph.db
    When "graph verify" runs
    Then it reports status "broken"
    And the first ChainBreak kind is "ROW_DELETED" at the gap
    And it exits 4

  Scenario: graph verify detects an inserted Event
    Given three chained Events exist
    And an extra row is inserted that is unreachable from the Genesis linkage walk
    When "graph verify" runs
    Then the first ChainBreak kind is "ROW_INSERTED"

  Scenario: graph verify detects reordered Events
    Given three chained Events whose self-hashes each recompute to their stored this_hash
    But whose prev_hash linkage no longer follows seq order
    When "graph verify" runs
    Then the first ChainBreak kind is "ROW_REORDERED"

  Scenario: Append-only is enforced
    Given the Event Log contains Events
    When any code path attempts an UPDATE or DELETE on the events table
    Then the attempt fails a test guard
    And no production path issues such a statement

  # ---------- Outcome Events (FR5) ----------

  Scenario: A failing test command is recorded as an Outcome Event
    Given a Bash payload running "go test ./..." whose tool_response reports exit code 1
    When capture is invoked
    Then an Outcome Event is appended with kind "outcome", is_test true, exit_code 1
    And exit_code_source is "reported"
    And the Outcome Event is chained like any other Event

  Scenario: An inferred exit code is marked as inferred
    Given a Bash payload running "make test" whose tool_response has no explicit exit field
    And whose output contains "FAIL"
    When capture is invoked
    Then the Outcome Event exit_code is non-zero
    And exit_code_source is "inferred"

  Scenario: A non-test Bash command records an exit code without test classification
    Given a Bash payload running "ls" whose tool_response reports exit code 0
    When capture is invoked
    Then an Outcome Event is appended with is_test false and is_build false

  # ---------- Reconciliation / Blind Spots (FR9) ----------

  Scenario: An out-of-band human edit is reconciled as source unknown
    Given the working tree changed with no corresponding capture Event
    When the Reconciliation pass runs
    Then an Out-of-Band Event is appended with source "unknown", kind "out-of-band"
    And it carries before_blob and after_blob File Blob Refs
    And it is chained into the same Event Log (not forged as an observed Event)

  Scenario: Reconciliation only covers the unexplained residual
    Given a file changed by a captured Edit Event
    And another file changed with no Event
    When the Reconciliation pass runs
    Then only the second file produces an Out-of-Band Event

  # ---------- Redaction (FR12, FR11) ----------

  Scenario: A secret token in the payload is redacted before storage
    Given a Bash payload whose command contains an AWS access key
    When capture is invoked
    Then payload_raw contains a Typed Placeholder "[REDACTED:aws-access-token]"
    And the raw key value never appears in graph.db

  Scenario: A sensitive file path is stored as a Redaction Digest
    Given an Edit payload whose file_path is ".env"
    When capture is invoked
    Then the Event stores a Redaction Digest (sha256 + length) instead of file contents
    And no .env line content appears in graph.db

  Scenario: An oversized payload is bounded
    Given a tool_response exceeding the payload size cap
    When capture is invoked
    Then the stored Event is truncated with the truncation flag set and payload_size recorded
    And this_hash is computed over exactly the stored bytes

  # ---------- Non-blocking (FR13) ----------

  Scenario: Capture never blocks on write-lock contention
    Given another writer holds the write lock beyond the busy timeout
    When capture is invoked
    Then capture skips appending the Event
    And it writes a warning to stderr
    And it exits 0 without blocking the agent

  Scenario: A malformed payload never errors the hook
    Given stdin contains the bytes `{not json` that fail JSON parse
    When capture is invoked
    Then no Event is appended
    And capture exits 0 with a stderr warning

  Scenario: Interactive invocation with no piped payload is a no-op for payload merge
    Given capture is run from an interactive terminal with no stdin payload
    When capture is invoked
    Then no payload fields are merged and behavior falls back to explicit flags

  # ---------- Projections & Rebuild (FR6, FR7, FR10) ----------

  Scenario: Projections rebuild deterministically from the Event Log
    Given a log of ten chained Events across two sessions
    And the sessions, actions, and action_modifies projections are dropped
    When "graph rebuild" runs
    Then the projections are reconstructed solely from the Event Log
    And a second rebuild produces byte-identical projections

  Scenario: Concurrent agents are attributed to separate sessions from one chain
    Given two agents capture interleaved Events with different instance_id values
    When the Events are appended to the single shared chain
    And the session projection is built
    Then each instance_id maps to its own session
    And the chain remains a single unforked sequence

  Scenario: Rebuild refuses on a broken chain
    Given an Event Log with a ChainBreak
    When "graph rebuild" runs
    Then it reports the break and refuses to rebuild projections

  # ---------- Provenance (FR14) ----------

  Scenario: Provenance shows rename-aware file history including out-of-band changes
    Given file "old.go" was edited by an Event, renamed to "new.go", then changed out-of-band
    When "graph provenance new.go" runs
    Then the output lists all three changes in chronological order
    And the out-of-band change is flagged with source "unknown"

  # ---------- Forensic diagnosis (FR15) ----------

  Scenario: Diagnose ranks the breaking action within the suspect window
    Given an Outcome Event at seq 804 with a passing test
    And an Outcome Event at seq 871 with the same test failing
    And three Edit Events in between touching the relevant file
    When "graph diagnose" runs for the failing symptom
    Then the Suspect Window is seq 805..870
    And the highest-ranked Candidate is the most recent Event directly touching the seed file
    And each Candidate carries before_blob and after_blob for direct diffing

  Scenario: Diagnose flags low confidence with no green baseline
    Given a failing Outcome Event with no prior passing Outcome Event for that test
    When "graph diagnose" runs
    Then the result is flagged low_confidence "no_green_baseline"
    And the Suspect Window opens at Genesis

  Scenario: Diagnose refuses on a tampered chain
    Given an Event Log with a ChainBreak
    When "graph diagnose" runs without --force
    Then it reports chain_verified false and exits 4

  Scenario: Diagnose LLM re-rank cannot add candidates
    Given a deterministic Candidate Ranking of five Events
    When the optional LLM re-rank runs over the top-N
    Then the result contains only Events from the deterministic set
    And the exit behavior is unchanged

  # ---------- Scope boundary ----------

  Scenario: The code index is unaffected by the capture redesign
    Given an indexed repository with commit, file, AST, and co-change data
    When the capture subsystem is replaced by the Event Log
    Then existing impact and index queries return the same results
```
