# Task 019: graph diagnose (impl)

**type**: impl
**depends-on**: ["018", "009", "011", "013"]

## Files
- create: `application/diagnose_service.go` — `DiagnoseService` (deterministic
  suspect-window Candidate Ranking + bounded LLM re-rank).
- modify: `cmd/diagnose.go` — replace the not-implemented stub with real wiring.

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

GREEN: make task-018 pass. Reference architecture.md "`graph diagnose
"<symptom>"` → `DiagnosisResult`" (steps 1-5, scoring weights), "Exit Codes" (4 =
chain integrity failure), NFR2 (placement), best-practices.md pitfall 13
(down-weight inferred reds). Reuses the verify chain (task-011 `VerifyChain`), the
Outcome Events (task-009), `ImpactService.Impact` / `CouplingStrength`, and the
projections (task-013).

Signatures only — NO bodies.

### `application/diagnose_service.go`

```go
package application

// Candidate is one ranked suspect Event within the Suspect Window.
type Candidate struct {
    Seq        int64
    EventID    string
    Tool       string
    Files      []string
    BeforeBlob string
    AfterBlob  string
    Score      float64
}

// DiagnosisResult is the forensic output of graph diagnose.
type DiagnosisResult struct {
    GreenSeq      int64   // last-green Outcome Event seq (0 if none)
    RedSeq        int64   // first-red Outcome Event seq
    GreenCommit   string
    RedCommit     string
    WindowSize    int
    Candidates    []Candidate // ranked, total order
    ChainVerified bool
    LowConfidence string      // e.g. "no_green_baseline", empty when high-confidence
}

// DiagnoseRequest carries the symptom and diagnose options.
type DiagnoseRequest struct {
    Symptom string
    Files   []string // --file seeds
    UseLLM  bool     // --llm
    Force   bool     // --force: proceed despite a ChainBreak
    TopN    int      // LLM re-rank bound
}

// DiagnoseService traces a symptom to the Candidate Event that most likely
// introduced it, over the Suspect Window between last-green and first-red
// Outcome Events.
type DiagnoseService struct {
    repo   graph.GraphRepository
    impact *ImpactService
    llm    DiagnoseReranker // bounded re-rank port; nil/no-endpoint => deterministic order is final
}

func NewDiagnoseService(repo graph.GraphRepository, impact *ImpactService, llm DiagnoseReranker) *DiagnoseService

// Diagnose runs the deterministic pipeline (verify → window → relevant set →
// score) and the optional bounded LLM re-rank.
func (s *DiagnoseService) Diagnose(ctx context.Context, req DiagnoseRequest) (*DiagnosisResult, error)

// DiagnoseReranker reorders, but never extends, the deterministic top-N.
type DiagnoseReranker interface {
    Rerank(ctx context.Context, symptom string, candidates []Candidate) ([]Candidate, error)
}
```

Ranking pipeline (prose, no bodies — architecture.md steps 1-5):

1. `repo.VerifyChain(ctx)` first; on a `ChainBreak`, set `ChainVerified=false`
   and, unless `req.Force`, return a typed error mapping to exit code 4.
2. **Suspect Window** from Outcome Events: `last_green = MAX(seq)` test outcome
   with `exit_code==0` matching the symptom; `first_red = MIN(seq) > last_green`
   with `exit_code!=0`. Window = Events strictly between them. No green baseline →
   window opens at Genesis, set `LowConfidence="no_green_baseline"`.
3. **Relevant file set R**: seeds (from `req.Files`, the symptom string, the
   failing test's session) expanded by `impact.Impact(ctx,
   graph.ImpactRequest{Paths: seeds, Depth: 1})` (reuses `CouplingStrength`).
4. **Candidates** = window Events touching R (rename-resolved via
   `ResolveRenames`), scored `0.35*recency + 0.25*impact_overlap +
   0.25*direct_seed_hit + 0.10*churn - 0.05*later_reverted`, `recency = (e.seq -
   last_green) / (first_red - last_green)`; total order, ties broken by higher
   `seq`. Down-weight inferred reds (best-practices.md pitfall 13). Populate each
   Candidate's `before_blob`/`after_blob` from `event_files`.
5. **Optional LLM re-rank** (`req.UseLLM` and `llm != nil`): pass only the top-N
   Candidates and the symptom to `llm.Rerank`; intersect the returned order back
   against the deterministic set so the LLM cannot add candidates or change exit
   behavior. `--no-llm`/no endpoint → deterministic order is final.

### `cmd/diagnose.go`

- Replace the stub `RunE` (currently prints "not yet implemented") with wiring
  (NFR2): parse `diagnose "<symptom>"` + flags (`--file`, `--llm`/`--no-llm`,
  `--force`), open the repo, construct `ImpactService`, the (optional) reranker,
  and `NewDiagnoseService`, call `Diagnose`, render `DiagnosisResult`, and exit
  with code 4 on the chain-break typed error (`pkg/errors`). No business logic in
  `cmd/`.

## Steps
1. Read architecture.md "graph diagnose" and "Exit Codes", best-practices.md
   pitfall 13.
2. Add the signatures above to `application/diagnose_service.go`; implement the
   verify→window→relevant-set→score pipeline and bounded LLM re-rank.
3. Replace the `cmd/diagnose.go` stub with real wiring + exit-code-4 mapping.
4. Run task-018; iterate to GREEN. Run `go build ./...` and `gofmt -l`.

## Verification
- `go test ./application/... -run TestDiagnose` — GREEN (task-018 passes).
- `go build ./...` — succeeds.
- `gofmt -l application/diagnose_service.go cmd/diagnose.go` — prints nothing.
- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...`
  — no regressions in previously passing tasks.
