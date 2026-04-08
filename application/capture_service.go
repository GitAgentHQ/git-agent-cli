package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gitagenthq/git-agent/domain/graph"
)

const (
	maxDiffBytes       = 100 * 1024 // 100KB
	sessionTimeoutMins = 30
)

// CaptureService records delta-based agent actions into the graph.
type CaptureService struct {
	repo graph.GraphRepository
	git  graph.GraphGitClient
}

// NewCaptureService creates a CaptureService with the given repository and git client.
func NewCaptureService(repo graph.GraphRepository, git graph.GraphGitClient) *CaptureService {
	return &CaptureService{repo: repo, git: git}
}

// Capture records a delta-based action. It detects which files changed since
// the last capture by comparing content hashes, then stores only the delta.
func (s *CaptureService) Capture(ctx context.Context, req graph.CaptureRequest) (*graph.CaptureResult, error) {
	start := time.Now()

	if req.EndSession {
		return s.endSession(ctx, req.Source, req.InstanceID, start)
	}

	changedFiles, err := s.git.DiffNameOnly(ctx)
	if err != nil {
		return nil, fmt.Errorf("diff name only: %w", err)
	}
	if len(changedFiles) == 0 {
		return &graph.CaptureResult{
			Skipped:   true,
			Reason:    "no changes detected",
			CaptureMs: time.Since(start).Milliseconds(),
		}, nil
	}

	// Hash every changed file.
	currentHashes := make(map[string]string, len(changedFiles))
	for _, f := range changedFiles {
		h, err := s.git.HashObject(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("hash object %s: %w", f, err)
		}
		currentHashes[f] = h
	}

	// Compare against the baseline to find true deltas.
	baselineHashes, err := s.repo.GetCaptureBaseline(ctx, changedFiles)
	if err != nil {
		return nil, fmt.Errorf("get capture baseline: %w", err)
	}

	var deltaFiles []string
	for _, f := range changedFiles {
		baseHash, exists := baselineHashes[f]
		if !exists || baseHash != currentHashes[f] {
			deltaFiles = append(deltaFiles, f)
		}
	}

	if len(deltaFiles) == 0 {
		return &graph.CaptureResult{
			Skipped:   true,
			Reason:    "no changes detected",
			CaptureMs: time.Since(start).Milliseconds(),
		}, nil
	}

	// Get the diff for delta files only.
	deltaDiff, err := s.git.DiffForFiles(ctx, deltaFiles)
	if err != nil {
		return nil, fmt.Errorf("diff for files: %w", err)
	}
	deltaDiff = truncateDiff(deltaDiff)

	// Find or create session.
	session, err := s.repo.GetActiveSession(ctx, req.Source, req.InstanceID, sessionTimeoutMins)
	if err != nil {
		return nil, fmt.Errorf("get active session: %w", err)
	}
	if session == nil {
		session = &graph.SessionNode{
			ID:         uuid.New().String(),
			Source:     req.Source,
			InstanceID: req.InstanceID,
			StartedAt:  time.Now().Unix(),
		}
		if err := s.repo.UpsertSession(ctx, *session); err != nil {
			return nil, fmt.Errorf("upsert session: %w", err)
		}
	}

	// Compute next sequence.
	count, err := s.repo.GetActionCountForSession(ctx, session.ID)
	if err != nil {
		return nil, fmt.Errorf("get action count: %w", err)
	}
	sequence := count + 1
	actionID := fmt.Sprintf("%s:%d", session.ID, sequence)

	action := graph.ActionNode{
		ID:           actionID,
		SessionID:    session.ID,
		Sequence:     sequence,
		Tool:         req.Tool,
		Diff:         deltaDiff,
		FilesChanged: deltaFiles,
		Timestamp:    time.Now().Unix(),
		Message:      req.Message,
	}
	if err := s.repo.CreateAction(ctx, action); err != nil {
		return nil, fmt.Errorf("create action: %w", err)
	}

	for _, f := range deltaFiles {
		if err := s.repo.CreateActionModifies(ctx, actionID, f, 0, 0); err != nil {
			return nil, fmt.Errorf("create action modifies: %w", err)
		}
	}

	// Update baseline for ALL changed files (not just delta).
	if err := s.repo.UpdateCaptureBaseline(ctx, currentHashes); err != nil {
		return nil, fmt.Errorf("update capture baseline: %w", err)
	}

	return &graph.CaptureResult{
		ActionID:     actionID,
		SessionID:    session.ID,
		FilesChanged: deltaFiles,
		CaptureMs:    time.Since(start).Milliseconds(),
	}, nil
}

// EndSession finds and ends the active session for the given source/instance.
func (s *CaptureService) EndSession(ctx context.Context, source, instanceID string) error {
	session, err := s.repo.GetActiveSession(ctx, source, instanceID, sessionTimeoutMins)
	if err != nil {
		return fmt.Errorf("get active session: %w", err)
	}
	if session == nil {
		return nil
	}
	return s.repo.EndSession(ctx, session.ID)
}

func (s *CaptureService) endSession(ctx context.Context, source, instanceID string, start time.Time) (*graph.CaptureResult, error) {
	if err := s.EndSession(ctx, source, instanceID); err != nil {
		return nil, err
	}
	return &graph.CaptureResult{
		Skipped:   true,
		Reason:    "session ended",
		CaptureMs: time.Since(start).Milliseconds(),
	}, nil
}

func truncateDiff(d string) string {
	if len(d) <= maxDiffBytes {
		return d
	}
	return d[:maxDiffBytes] + "\n[truncated]"
}
