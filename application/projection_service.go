package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// ProjectionRebuilder is the cold-path Replay engine. It regenerates the derived
// Projections (sessions, actions, action_modifies, event_files) solely from the
// append-only Event Log, so the log stays the single source of truth and the
// hot path can append without touching git or projections.
type ProjectionRebuilder struct {
	repo graph.GraphRepository
	git  graph.GraphGitClient
}

// NewProjectionRebuilder creates a ProjectionRebuilder over the Event Log
// repository and the git client used for cold-path File Blob Refs.
func NewProjectionRebuilder(repo graph.GraphRepository, git graph.GraphGitClient) *ProjectionRebuilder {
	return &ProjectionRebuilder{repo: repo, git: git}
}

// projectionSession accumulates the running state of one open session as Events
// are folded in seq order.
type projectionSession struct {
	id         string
	source     string
	instanceID string
	startedAt  int64
	lastAt     int64
	nextSeq    int
}

// Rebuild verifies the chain, resets the derived tables, then streams the Event
// Log in seq order and folds it into the Projections. Ordering and timestamps
// derive solely from Event fields plus chain order — no wall-clock reads, no
// map-iteration ordering — so two rebuilds produce byte-identical Projections.
func (r *ProjectionRebuilder) Rebuild(ctx context.Context) error {
	vr, err := r.repo.VerifyChain(ctx)
	if err != nil {
		return fmt.Errorf("verify chain: %w", err)
	}
	if vr.Status != "ok" {
		if vr.FirstBreak != nil {
			b := vr.FirstBreak
			return fmt.Errorf("refusing to rebuild: chain broken (%s) at seq %d (event %s)",
				b.Kind, b.Seq, b.EventID)
		}
		return fmt.Errorf("refusing to rebuild: chain status %q", vr.Status)
	}

	if err := r.repo.ResetProjections(ctx); err != nil {
		return fmt.Errorf("reset projections: %w", err)
	}

	cur, err := r.repo.StreamEvents(ctx, 0)
	if err != nil {
		return fmt.Errorf("stream events: %w", err)
	}
	defer cur.Close()

	// open holds the currently-open session per (source, instance_id) key. A new
	// session opens for that key when the inter-Event gap exceeds the timeout.
	open := make(map[string]*projectionSession)
	timeoutSecs := int64(sessionTimeoutMins * 60)

	for cur.Next() {
		e := cur.Event()
		key := string(e.Source) + "\x00" + e.InstanceID

		sess := open[key]
		if sess == nil || e.RecordedAt-sess.lastAt > timeoutSecs {
			sess = &projectionSession{
				id:         sessionID(string(e.Source), e.InstanceID, e.Seq),
				source:     string(e.Source),
				instanceID: e.InstanceID,
				startedAt:  e.RecordedAt,
				lastAt:     e.RecordedAt,
				nextSeq:    1,
			}
			open[key] = sess
			if err := r.repo.UpsertSession(ctx, graph.SessionNode{
				ID:         sess.id,
				Source:     sess.source,
				InstanceID: sess.instanceID,
				StartedAt:  sess.startedAt,
			}); err != nil {
				return fmt.Errorf("upsert session: %w", err)
			}
		}
		sess.lastAt = e.RecordedAt

		if err := r.foldEvent(ctx, e, sess); err != nil {
			return err
		}
	}
	if err := cur.Err(); err != nil {
		return fmt.Errorf("replay events: %w", err)
	}
	return nil
}

// foldEvent projects a single Event into its action row and its touched-file
// rows (action_modifies + event_files with cold-path File Blob Refs).
func (r *ProjectionRebuilder) foldEvent(ctx context.Context, e graph.EventRecord, sess *projectionSession) error {
	files := extractFileChanges(e)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.path)
	}

	action := graph.ActionNode{
		ID:           fmt.Sprintf("%s:%d", sess.id, sess.nextSeq),
		SessionID:    sess.id,
		Sequence:     sess.nextSeq,
		Tool:         e.ToolName,
		FilesChanged: paths,
		Timestamp:    e.RecordedAt,
		Message:      e.Command,
	}
	sess.nextSeq++

	if err := r.repo.CreateAction(ctx, action); err != nil {
		return fmt.Errorf("create action: %w", err)
	}

	for _, f := range files {
		if err := r.repo.CreateActionModifies(ctx, action.ID, f.path, f.additions, f.deletions); err != nil {
			return fmt.Errorf("create action_modifies: %w", err)
		}
		afterBlob, err := r.git.HashObject(ctx, f.path)
		if err != nil {
			return fmt.Errorf("hash object %s: %w", f.path, err)
		}
		if err := r.repo.CreateEventFile(ctx, graph.EventFile{
			EventSeq:   e.Seq,
			FilePath:   f.path,
			AfterBlob:  afterBlob,
			ChangeKind: f.changeKind,
			Additions:  f.additions,
			Deletions:  f.deletions,
		}); err != nil {
			return fmt.Errorf("create event_file: %w", err)
		}
	}
	return nil
}

// sessionID is a deterministic id derived solely from Event fields, so a rebuild
// always reproduces the same id for the same session.
func sessionID(source, instanceID string, firstSeq int64) string {
	return fmt.Sprintf("%s:%s:%d", source, instanceID, firstSeq)
}

// fileChange is a file touched by an Event, with line counts derived from the
// payload content.
type fileChange struct {
	path       string
	additions  int
	deletions  int
	changeKind string
}

// payloadToolInput is the slice of a redacted PostToolUse payload the projection
// fold reads: tool_input file edits for Edit/Write/MultiEdit.
type payloadToolInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath  string `json:"file_path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Content   string `json:"content"`
		Edits     []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	} `json:"tool_input"`
}

// extractFileChanges derives the touched files and line counts from an Event's
// payload. Edit/Write set file_path; MultiEdit folds each edit's line delta onto
// the one file_path. Non-file tools (Bash, outcomes) touch no files.
func extractFileChanges(e graph.EventRecord) []fileChange {
	if e.Kind != graph.EventKindTool {
		return nil
	}
	var p payloadToolInput
	if err := json.Unmarshal(e.PayloadRaw, &p); err != nil {
		return nil
	}
	if p.ToolInput.FilePath == "" {
		return nil
	}

	fc := fileChange{path: p.ToolInput.FilePath, changeKind: "M"}
	switch e.ToolName {
	case "Write":
		fc.additions = countLines(p.ToolInput.Content)
		fc.changeKind = "A"
	case "MultiEdit":
		for _, ed := range p.ToolInput.Edits {
			fc.additions += countLines(ed.NewString)
			fc.deletions += countLines(ed.OldString)
		}
	default: // Edit and Edit-shaped tools
		fc.additions = countLines(p.ToolInput.NewString)
		fc.deletions = countLines(p.ToolInput.OldString)
	}
	return []fileChange{fc}
}

// countLines counts the lines of content for an addition/deletion total. Empty
// content is zero lines; a non-empty string is at least one line.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
