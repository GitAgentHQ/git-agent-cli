package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

// diagnoseFakeRepo is a programmable graph.GraphRepository for diagnose: it
// serves a fixed Event stream, a verify result, rename resolution, file-change
// blob refs, and an impact result. It embeds the interface so only the methods
// diagnose exercises need real bodies.
type diagnoseFakeRepo struct {
	graph.GraphRepository
	events    []graph.EventRecord
	verify    graph.VerifyResult
	renames   map[string][]string
	fileBlobs map[int64]map[string]blobPair // seq -> file -> before/after
	impact    *graph.ImpactResult
}

type blobPair struct{ before, after string }

func (r *diagnoseFakeRepo) VerifyChain(context.Context) (graph.VerifyResult, error) {
	return r.verify, nil
}

func (r *diagnoseFakeRepo) StreamEvents(_ context.Context, sinceSeq int64) (graph.EventCursor, error) {
	var filtered []graph.EventRecord
	for _, e := range r.events {
		if e.Seq > sinceSeq {
			filtered = append(filtered, e)
		}
	}
	return &sliceEventCursor{events: filtered, idx: -1}, nil
}

func (r *diagnoseFakeRepo) ResolveRenames(_ context.Context, filePath string) ([]string, error) {
	return r.renames[filePath], nil
}

func (r *diagnoseFakeRepo) Impact(_ context.Context, _ graph.ImpactRequest) (*graph.ImpactResult, error) {
	if r.impact != nil {
		return r.impact, nil
	}
	return &graph.ImpactResult{}, nil
}

func (r *diagnoseFakeRepo) FileChanges(_ context.Context, filePaths []string) ([]graph.FileChangeRow, error) {
	want := map[string]bool{}
	for _, p := range filePaths {
		want[p] = true
	}
	var out []graph.FileChangeRow
	for _, e := range r.events {
		fb := r.fileBlobs[e.Seq]
		for file, bp := range fb {
			if !want[file] {
				continue
			}
			out = append(out, graph.FileChangeRow{
				Seq:        e.Seq,
				EventID:    e.EventID,
				RecordedAt: e.RecordedAt,
				Source:     string(e.Source),
				ToolName:   e.ToolName,
				FilePath:   file,
				BeforeBlob: bp.before,
				AfterBlob:  bp.after,
				ChangeKind: "M",
			})
		}
	}
	return out, nil
}

// sliceEventCursor walks a slice of Events as a graph.EventCursor.
type sliceEventCursor struct {
	events []graph.EventRecord
	idx    int
}

func (c *sliceEventCursor) Next() bool {
	c.idx++
	return c.idx < len(c.events)
}
func (c *sliceEventCursor) Event() graph.EventRecord { return c.events[c.idx] }
func (c *sliceEventCursor) Err() error               { return nil }
func (c *sliceEventCursor) Close() error             { return nil }

// fakeReranker reorders the deterministic top-N and tries to smuggle in an
// invented Candidate; the service must drop anything not already in the set.
type fakeReranker struct {
	called bool
}

func (r *fakeReranker) Rerank(_ context.Context, _ string, candidates []application.Candidate) ([]application.Candidate, error) {
	r.called = true
	reversed := make([]application.Candidate, 0, len(candidates)+1)
	// Inject a forged candidate the deterministic set never produced.
	reversed = append(reversed, application.Candidate{Seq: 999999, EventID: "forged", Score: 1.0})
	for i := len(candidates) - 1; i >= 0; i-- {
		reversed = append(reversed, candidates[i])
	}
	return reversed, nil
}

// outcomeEvent builds an Outcome Event with the given test exit code.
func outcomeEvent(seq int64, recordedAt int64, exitCode int, testName string) graph.EventRecord {
	code := exitCode
	return graph.EventRecord{
		Seq:            seq,
		EventID:        testName,
		RecordedAt:     recordedAt,
		Source:         graph.EventSourceClaudeCode,
		InstanceID:     "agent-A",
		Kind:           graph.EventKindOutcome,
		ToolName:       "Bash",
		Command:        "go test ./...",
		ExitCode:       &code,
		ExitCodeSource: "reported",
		IsTest:         true,
		TestName:       testName,
	}
}

// editEvent builds an Edit Event at seq touching one file.
func editEvent(seq int64, recordedAt int64, file string) graph.EventRecord {
	return graph.EventRecord{
		Seq:        seq,
		EventID:    file,
		RecordedAt: recordedAt,
		Source:     graph.EventSourceClaudeCode,
		InstanceID: "agent-A",
		Kind:       graph.EventKindTool,
		ToolName:   "Edit",
		PayloadRaw: editPayload(file, "x", "y"),
	}
}

// seedSuspectWindow builds the canonical scenario: green test at seq 804, red
// test at seq 871, three Edit Events in between touching the seed file.
func seedSuspectWindow() *diagnoseFakeRepo {
	repo := &diagnoseFakeRepo{
		verify:    graph.VerifyResult{Status: "ok"},
		renames:   map[string][]string{},
		fileBlobs: map[int64]map[string]blobPair{},
	}
	repo.events = []graph.EventRecord{
		outcomeEvent(804, 8040, 0, "TestThing"),
		editEvent(820, 8200, "seed.go"),
		editEvent(840, 8400, "other.go"),
		editEvent(860, 8600, "seed.go"),
		outcomeEvent(871, 8710, 1, "TestThing"),
	}
	repo.fileBlobs[820] = map[string]blobPair{"seed.go": {"b820", "a820"}}
	repo.fileBlobs[840] = map[string]blobPair{"other.go": {"b840", "a840"}}
	repo.fileBlobs[860] = map[string]blobPair{"seed.go": {"b860", "a860"}}
	return repo
}

func newDiagnoseSvc(repo graph.GraphRepository, llm application.DiagnoseReranker) *application.DiagnoseService {
	return application.NewDiagnoseService(repo, application.NewImpactService(repo), llm)
}

func TestDiagnose_SuspectWindowBetweenGreenAndRed(t *testing.T) {
	ctx := context.Background()
	repo := seedSuspectWindow()

	res, err := newDiagnoseSvc(repo, nil).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
	})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if res.GreenSeq != 804 {
		t.Errorf("GreenSeq = %d, want 804", res.GreenSeq)
	}
	if res.RedSeq != 871 {
		t.Errorf("RedSeq = %d, want 871", res.RedSeq)
	}
	// Window is strictly between last-green and first-red: seq 805..870.
	for _, c := range res.Candidates {
		if c.Seq <= 804 || c.Seq >= 871 {
			t.Errorf("candidate seq %d outside window 805..870", c.Seq)
		}
	}
}

func TestDiagnose_HighestRankedIsMostRecentSeedTouch(t *testing.T) {
	ctx := context.Background()
	repo := seedSuspectWindow()

	res, err := newDiagnoseSvc(repo, nil).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
	})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(res.Candidates) == 0 {
		t.Fatal("no candidates ranked")
	}
	// seq 860 is the most recent window Event directly touching seed.go.
	if res.Candidates[0].Seq != 860 {
		t.Errorf("top candidate seq = %d, want 860 (most recent seed touch)", res.Candidates[0].Seq)
	}
}

func TestDiagnose_CandidatesCarryBlobRefs(t *testing.T) {
	ctx := context.Background()
	repo := seedSuspectWindow()

	res, err := newDiagnoseSvc(repo, nil).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
	})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(res.Candidates) == 0 {
		t.Fatal("no candidates ranked")
	}
	top := res.Candidates[0]
	if top.BeforeBlob != "b860" || top.AfterBlob != "a860" {
		t.Errorf("top candidate blobs = %q->%q, want b860->a860", top.BeforeBlob, top.AfterBlob)
	}
}

func TestDiagnose_RelevantSetExpandedByImpact(t *testing.T) {
	ctx := context.Background()
	repo := seedSuspectWindow()
	// Impact expands seed.go to include other.go as a co-changed neighbour, so the
	// Edit on other.go at seq 840 must become a Candidate (in relevant set R).
	repo.impact = &graph.ImpactResult{
		Targets:   []string{"seed.go"},
		CoChanged: []graph.ImpactEntry{{Path: "other.go", CouplingStrength: 0.9, Score: 0.9}},
	}

	res, err := newDiagnoseSvc(repo, nil).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
	})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	found := false
	for _, c := range res.Candidates {
		if c.Seq == 840 {
			found = true
		}
	}
	if !found {
		t.Errorf("co-changed neighbour edit (seq 840) not in candidates: %+v", res.Candidates)
	}
}

func TestDiagnose_NoGreenBaselineLowConfidence(t *testing.T) {
	ctx := context.Background()
	repo := &diagnoseFakeRepo{
		verify:    graph.VerifyResult{Status: "ok"},
		renames:   map[string][]string{},
		fileBlobs: map[int64]map[string]blobPair{},
	}
	// Only a failing Outcome Event, no prior green for the test.
	repo.events = []graph.EventRecord{
		editEvent(10, 100, "seed.go"),
		outcomeEvent(20, 200, 1, "TestThing"),
	}
	repo.fileBlobs[10] = map[string]blobPair{"seed.go": {"b10", "a10"}}

	res, err := newDiagnoseSvc(repo, nil).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
	})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if res.LowConfidence != "no_green_baseline" {
		t.Errorf("LowConfidence = %q, want no_green_baseline", res.LowConfidence)
	}
	if res.GreenSeq != 0 {
		t.Errorf("GreenSeq = %d, want 0 (window opens at Genesis)", res.GreenSeq)
	}
	// The pre-red Edit at seq 10 is in the genesis-opened window.
	found := false
	for _, c := range res.Candidates {
		if c.Seq == 10 {
			found = true
		}
	}
	if !found {
		t.Errorf("seq 10 edit not in genesis-opened window: %+v", res.Candidates)
	}
}

func TestDiagnose_RefusesOnChainBreak(t *testing.T) {
	ctx := context.Background()
	repo := seedSuspectWindow()
	repo.verify = graph.VerifyResult{
		Status:     "broken",
		FirstBreak: &graph.ChainBreak{Seq: 830, Kind: graph.ChainBreakRowEdited},
	}

	// Without --force: refusal mapping to exit code 4.
	_, err := newDiagnoseSvc(repo, nil).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
	})
	if err == nil {
		t.Fatal("Diagnose on broken chain returned nil error, want refusal")
	}
	var exitErr *agentErrors.ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 4 {
		t.Errorf("error %v does not map to exit code 4", err)
	}

	// With --force: proceeds, flagging chain_verified false.
	res, err := newDiagnoseSvc(repo, nil).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
		Force:   true,
	})
	if err != nil {
		t.Fatalf("Diagnose --force: %v", err)
	}
	if res.ChainVerified {
		t.Error("ChainVerified must be false on a broken chain even with --force")
	}
}

func TestDiagnose_LLMRerankCannotAddCandidates(t *testing.T) {
	ctx := context.Background()
	repo := seedSuspectWindow()
	rr := &fakeReranker{}

	res, err := newDiagnoseSvc(repo, rr).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
		UseLLM:  true,
		TopN:    5,
	})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if !rr.called {
		t.Error("reranker was not invoked despite UseLLM")
	}
	// The forged candidate (seq 999999) must not survive.
	for _, c := range res.Candidates {
		if c.Seq == 999999 || c.EventID == "forged" {
			t.Errorf("forged candidate leaked into result: %+v", c)
		}
	}
	// Every result candidate must be a real window Event.
	for _, c := range res.Candidates {
		if c.Seq <= 804 || c.Seq >= 871 {
			t.Errorf("result candidate seq %d not from deterministic window", c.Seq)
		}
	}
}

// failingReranker always errors, simulating a transient LLM failure (bad JSON,
// network timeout, empty order). Diagnose must degrade to the deterministic
// order with a warning rather than failing the whole command.
type failingReranker struct{}

func (failingReranker) Rerank(_ context.Context, _ string, _ []application.Candidate) ([]application.Candidate, error) {
	return nil, errors.New("llm unavailable")
}

func TestDiagnose_LLMRerankFailureDegradesToDeterministic(t *testing.T) {
	ctx := context.Background()
	repo := seedSuspectWindow()

	res, err := newDiagnoseSvc(repo, failingReranker{}).Diagnose(ctx, application.DiagnoseRequest{
		Symptom: "TestThing",
		Files:   []string{"seed.go"},
		UseLLM:  true,
		TopN:    5,
	})
	if err != nil {
		t.Fatalf("rerank failure must not fail Diagnose, got: %v", err)
	}
	if len(res.Candidates) == 0 {
		t.Fatal("expected deterministic candidates despite rerank failure")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected a warning explaining the rerank fallback")
	}
	// The deterministic top candidate (most recent seed touch, seq 860) must
	// still rank first when the LLM could not reorder.
	if res.Candidates[0].Seq != 860 {
		t.Errorf("top candidate seq = %d, want 860 (deterministic order)", res.Candidates[0].Seq)
	}
	// Every candidate must remain a real window Event.
	for _, c := range res.Candidates {
		if c.Seq <= 804 || c.Seq >= 871 {
			t.Errorf("result candidate seq %d not from deterministic window", c.Seq)
		}
	}
}
