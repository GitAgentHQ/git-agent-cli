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

	// lastTouch records the highest seq touching each path. The working tree only
	// reflects a path's final state, so only that last Event may derive an
	// after_blob from git; deriving one for an earlier edit would fabricate the
	// current content as if it were historical.
	lastTouch, err := r.lastTouchSeqByPath(ctx)
	if err != nil {
		return err
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
	// lastBlob tracks the most recent after_blob per file_path as Events fold in
	// seq order, supplying before_blob for the next change on that path.
	lastBlob := make(map[string]string)

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

		if err := r.foldEvent(ctx, e, sess, lastBlob, lastTouch); err != nil {
			return err
		}
	}
	if err := cur.Err(); err != nil {
		return fmt.Errorf("replay events: %w", err)
	}
	return nil
}

// lastTouchSeqByPath streams the Event Log once and records, per file_path, the
// highest seq that touches it. Only that Event reflects the current working tree,
// so it is the only one allowed to derive an after_blob via git.HashObject.
func (r *ProjectionRebuilder) lastTouchSeqByPath(ctx context.Context) (map[string]int64, error) {
	cur, err := r.repo.StreamEvents(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("stream events (pre-scan): %w", err)
	}
	defer cur.Close()

	last := make(map[string]int64)
	for cur.Next() {
		e := cur.Event()
		for _, f := range extractEventFileProjections(e) {
			if e.Seq > last[f.path] {
				last[f.path] = e.Seq
			}
		}
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("pre-scan events: %w", err)
	}
	return last, nil
}

// foldEvent projects a single Event into its action row and its touched-file
// rows (action_modifies + event_files with cold-path File Blob Refs).
func (r *ProjectionRebuilder) foldEvent(ctx context.Context, e graph.EventRecord, sess *projectionSession, lastBlob map[string]string, lastTouch map[string]int64) error {
	files := extractEventFileProjections(e)
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

		before := f.beforeBlob
		if before == "" {
			before = lastBlob[f.path]
		}
		after := f.afterBlob
		if after == "" && e.Seq == lastTouch[f.path] {
			// Only the final touch of this path matches the working tree; earlier
			// edits leave after_blob unknown rather than fabricating current content.
			blob, herr := r.git.HashObject(ctx, f.path)
			if herr != nil {
				return fmt.Errorf("hash object %s: %w", f.path, herr)
			}
			after = blob
		}

		if err := r.repo.CreateEventFile(ctx, graph.EventFile{
			EventSeq:   e.Seq,
			FilePath:   f.path,
			BeforeBlob: before,
			AfterBlob:  after,
			ChangeKind: f.changeKind,
			Additions:  f.additions,
			Deletions:  f.deletions,
		}); err != nil {
			return fmt.Errorf("create event_file: %w", err)
		}
		lastBlob[f.path] = after
	}
	return nil
}

// sessionID is a deterministic id derived solely from Event fields, so a rebuild
// always reproduces the same id for the same session.
func sessionID(source, instanceID string, firstSeq int64) string {
	return fmt.Sprintf("%s:%s:%d", source, instanceID, firstSeq)
}

// eventFileProjection is one touched-file row derived from an Event during Replay.
type eventFileProjection struct {
	path       string
	beforeBlob string // literal OID; empty means use lastBlob[path]
	afterBlob  string // literal OID; empty means git.HashObject
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

// extractEventFileProjections derives touched files from an Event. Tool Events
// carry line counts from payload content; out-of-band Events carry literal OIDs.
// Agent tooling metadata (.git-agent/, .claude/) is excluded on every path so it
// never enters co-change, impact, or provenance — matching reconcile's filter.
func extractEventFileProjections(e graph.EventRecord) []eventFileProjection {
	var projs []eventFileProjection
	switch e.Kind {
	case graph.EventKindOutOfBand:
		projs = extractOutOfBandFileProjections(e)
	case graph.EventKindTool:
		projs = extractToolFileProjections(e)
	default:
		return nil
	}

	out := projs[:0]
	for _, p := range projs {
		if graph.IsToolingPath(p.path) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func extractOutOfBandFileProjections(e graph.EventRecord) []eventFileProjection {
	var p outOfBandPayload
	if err := json.Unmarshal(e.PayloadRaw, &p); err != nil {
		return nil
	}
	if p.OutOfBand.FilePath == "" {
		return nil
	}
	return []eventFileProjection{{
		path:       p.OutOfBand.FilePath,
		beforeBlob: p.OutOfBand.BeforeBlob,
		afterBlob:  p.OutOfBand.AfterBlob,
		changeKind: "M",
	}}
}

// extractToolFileProjections derives the touched files and line counts from a
// tool Event payload. Edit/Write set file_path; MultiEdit folds each edit's line
// delta onto the one file_path. Non-file tools (Bash, outcomes) touch no files.
func extractToolFileProjections(e graph.EventRecord) []eventFileProjection {
	var p payloadToolInput
	if err := json.Unmarshal(e.PayloadRaw, &p); err != nil {
		return nil
	}
	if p.ToolInput.FilePath == "" {
		return nil
	}

	fc := eventFileProjection{path: p.ToolInput.FilePath, changeKind: "M"}
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
	return []eventFileProjection{fc}
}

// fileChange is a file touched by an Event, with line counts for diagnose scoring.
type fileChange struct {
	path       string
	additions  int
	deletions  int
	changeKind string
}

// extractFileChanges returns touched files for callers that only need paths and
// line counts (e.g. diagnose scoring).
func extractFileChanges(e graph.EventRecord) []fileChange {
	projs := extractEventFileProjections(e)
	out := make([]fileChange, len(projs))
	for i, p := range projs {
		out[i] = fileChange{
			path:       p.path,
			additions:  p.additions,
			deletions:  p.deletions,
			changeKind: p.changeKind,
		}
	}
	return out
}

// countLines counts the lines of content for an addition/deletion total. Empty
// content is zero lines; a non-empty string is at least one line.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
