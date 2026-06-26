# Task 018: graph diagnose (test)

**type**: test
**depends-on**: ["001"]

## Files
- create: `application/diagnose_service_test.go` — RED unit tests for the
  deterministic suspect-window ranking and the bounded LLM re-rank, using a fake
  repository and a fake LLM client.

## BDD Scenario(s)
```gherkin
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
```

## What to implement

RED unit tests only. They must compile against the Task 001 contracts but FAIL
because `DiagnoseService` does not yet exist (task-019). Reference architecture.md
"`graph diagnose "<symptom>"` → `DiagnosisResult`" for the pipeline and the exact
scoring weights, "Exit Codes" (4 = chain integrity failure), and best-practices.md
pitfall 13 (down-weight inferred reds).

Use test doubles for all external dependencies — a fake `graph.GraphRepository`
(or a real temp SQLite db seeded with the fixtures) plus a fake LLM client; no
real network. Isolate from real git/LLM/endpoints.

### A. Deterministic suspect-window ranking — `diagnose_service_test.go`

- `TestDiagnose_SuspectWindowBetweenGreenAndRed`: seed a green Outcome Event at
  `seq 804` (`exit_code==0`, matching the symptom's test) and a red Outcome Event
  at `seq 871` (`exit_code!=0`, same test); seed three Edit Events at seqs in
  between touching the seed file. Assert the Suspect Window is `seq 805..870`
  (strictly between last-green and first-red, architecture.md step 2).
- `TestDiagnose_HighestRankedIsMostRecentSeedTouch`: with deterministic scoring
  `0.35*recency + 0.25*impact_overlap + 0.25*direct_seed_hit + 0.10*churn -
  0.05*later_reverted`, `recency = (e.seq - last_green) / (first_red -
  last_green)`, ties broken by higher `seq` (architecture.md step 4): assert the
  top Candidate is the most recent window Event directly touching the seed file.
- `TestDiagnose_CandidatesCarryBlobRefs`: each ranked Candidate carries
  `before_blob`/`after_blob` for direct diffing (architecture.md `DiagnosisResult`
  + scenario "each Candidate carries before_blob and after_blob").
- `TestDiagnose_RelevantSetExpandedByImpact`: seeds (from `--file`, the symptom
  string, the failing test's session) are expanded via
  `ImpactService.Impact({Paths:seeds, Depth:1})` (architecture.md step 3, reuses
  `CouplingStrength`); assert a co-changed neighbour returned by the (fake/seeded)
  impact result becomes part of relevant set R and influences `impact_overlap`.

### B. No green baseline — `diagnose_service_test.go`

- `TestDiagnose_NoGreenBaselineLowConfidence`: a failing Outcome Event with no
  prior passing Outcome Event for that test → result flagged
  `low_confidence: "no_green_baseline"` and the Suspect Window opens at Genesis
  (architecture.md step 2, scenario "Diagnose flags low confidence").

### C. Refuse on tampered chain — `diagnose_service_test.go`

- `TestDiagnose_RefusesOnChainBreak`: the repo's `VerifyChain` returns a broken
  `VerifyResult` (a `ChainBreak`); running diagnose without `--force` returns
  `chain_verified == false` and the typed error mapping to exit code **4**
  (architecture.md "Exit Codes": 4 = chain integrity failure; step 1 "Run verify
  internally; refuse (exit 4) on a break unless --force"). Assert `--force`
  bypasses the refusal.

### D. LLM re-rank bounded to deterministic set — `diagnose_service_test.go`

- `TestDiagnose_LLMRerankCannotAddCandidates`: a fake LLM client that attempts to
  return extra/invented candidates; assert the final result contains only Events
  from the deterministic top-N set (the LLM may reorder within N but cannot add
  candidates) and the exit behavior is unchanged (architecture.md step 5).
- Construct via the task-019 constructor (e.g. `NewDiagnoseService(repo, impact,
  llm)`); the call site does not yet exist, so the tests fail to build/run RED.

## Steps
1. Read architecture.md "graph diagnose" (steps 1-5, scoring weights, exit code
   4) and best-practices.md pitfall 13.
2. Build a fake `graph.GraphRepository` exposing `VerifyChain`, `StreamEvents`,
   `ResolveRenames`, and a fake LLM client; seed the green/red/edit-event
   fixtures and the impact result.
3. Write the four test groups above referencing the `DiagnoseService` constructor
   and `DiagnosisResult` type from task-019.
4. Run the package tests; confirm each fails because `DiagnoseService` is
   undefined, not for unrelated compile errors.

## Verification
- `go test ./application/...` — RED: tests fail to build/run because
  `DiagnoseService` (constructor + `DiagnosisResult`) does not exist yet. Confirm
  each failure cites the missing diagnose symbol, not unrelated errors.
