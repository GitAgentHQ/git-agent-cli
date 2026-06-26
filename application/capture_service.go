package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

const sessionTimeoutMins = 30

// CaptureService appends observed agent actions to the hash-chained Event Log.
// The hot path does one thing: build the EventRecord and append. The chain
// head read, seq assignment, and this_hash computation all live inside
// AppendEvent's transaction so there is one place that computes the hash. All
// git/diff/projection work lives on the cold Enrichment path.
type CaptureService struct {
	repo  graph.GraphRepository
	git   graph.GraphGitClient
	idGen graph.SessionIDGenerator
}

// NewCaptureService creates a CaptureService. git is retained only for the cold
// Enrichment/Reconciliation paths; the hot path never calls it.
func NewCaptureService(repo graph.GraphRepository, git graph.GraphGitClient, idGen graph.SessionIDGenerator) *CaptureService {
	return &CaptureService{repo: repo, git: git, idGen: idGen}
}

// Capture appends one Event to the chain. It builds the record from req.Event
// (already redacted upstream) and appends it; AppendEvent reads the chain head,
// assigns seq, and computes prev/this hash inside one transaction. On lock
// contention it skips without error so the agent is never blocked.
func (s *CaptureService) Capture(ctx context.Context, req graph.CaptureRequest) (*graph.CaptureResult, error) {
	start := time.Now()

	if req.EndSession {
		return &graph.CaptureResult{
			Skipped:   true,
			Reason:    "session ended",
			CaptureMs: time.Since(start).Milliseconds(),
		}, nil
	}

	if req.Event == nil {
		return &graph.CaptureResult{
			Skipped:   true,
			Reason:    "no payload observed",
			CaptureMs: time.Since(start).Milliseconds(),
		}, nil
	}

	e := *req.Event
	e.EventID = s.idGen.NewSessionID()
	if e.RecordedAt == 0 {
		e.RecordedAt = time.Now().Unix()
	}

	if e.ToolName == "Bash" && e.Command != "" {
		classifyOutcome(&e, req.ToolResponse)
	}

	persisted, err := s.repo.AppendEvent(ctx, e)
	if err != nil {
		if errors.Is(err, graph.ErrChainBusy) {
			return &graph.CaptureResult{
				Skipped:   true,
				Reason:    "event chain busy",
				CaptureMs: time.Since(start).Milliseconds(),
			}, nil
		}
		return nil, fmt.Errorf("append event: %w", err)
	}

	return &graph.CaptureResult{
		EventID:   persisted.EventID,
		Seq:       persisted.Seq,
		Source:    string(persisted.Source),
		CaptureMs: time.Since(start).Milliseconds(),
	}, nil
}

// classifyOutcome promotes a Bash tool Event to an Outcome Event in place. It
// classifies the command as test/build, resolves the exit code from the
// tool_response (reported) or infers it from failure markers (inferred), and
// records the result on e. Parse failure never fabricates an exit code: with no
// reported code and no failure marker the outcome is a reported-clean success
// only when the response itself states it, else an inferred success (0).
func classifyOutcome(e *graph.EventRecord, toolResponse []byte) {
	e.Kind = graph.EventKindOutcome

	cls := ClassifyCommand(e.Command)
	e.IsTest = cls.IsTest
	e.IsBuild = cls.IsBuild
	e.TestName = cls.TestName

	if code, ok := ExtractReportedExitCode(toolResponse); ok {
		e.ExitCode = &code
		e.ExitCodeSource = "reported"
		return
	}

	code, _ := InferExitCode(toolResponse)
	e.ExitCode = &code
	e.ExitCodeSource = "inferred"
}

// EndSession is retained for the cmd-layer end-session hook. Session boundaries
// are derived on the cold projection path from inter-Event gaps, so there is no
// live session row to close; this is a no-op on the append-only hot path.
func (s *CaptureService) EndSession(ctx context.Context, source, instanceID string) error {
	return nil
}
