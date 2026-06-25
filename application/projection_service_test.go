package application_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infragraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// newProjectionTestRepo opens a real on-disk SQLiteRepository over a temp
// graph.db so the fold exercises the actual schema, not a stand-in.
func newProjectionTestRepo(t *testing.T) *infragraph.SQLiteRepository {
	t.Helper()
	ctx := context.Background()
	client := infragraph.NewSQLiteClient(filepath.Join(t.TempDir(), "graph.db"))
	repo := infragraph.NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return repo
}

// editPayload builds a redacted-shape PostToolUse payload for an Edit tool call.
func editPayload(file, oldStr, newStr string) []byte {
	return []byte(fmt.Sprintf(
		`{"session_id":"x","hook_event_name":"PostToolUse","tool_name":"Edit",`+
			`"tool_input":{"file_path":%q,"old_string":%q,"new_string":%q}}`,
		file, oldStr, newStr,
	))
}

// seededEvent appends one Edit Event with the given attribution and recorded_at
// through the real append path, so prev_hash/this_hash/seq are genuine.
func seededEvent(t *testing.T, repo *infragraph.SQLiteRepository, source graph.EventSource, instanceID string, recordedAt int64, file, oldStr, newStr string) graph.EventRecord {
	t.Helper()
	rec := graph.EventRecord{
		EventID:    fmt.Sprintf("evt-%s-%d", instanceID, recordedAt),
		RecordedAt: recordedAt,
		Source:     source,
		InstanceID: instanceID,
		Kind:       graph.EventKindTool,
		ToolName:   "Edit",
		PayloadRaw: editPayload(file, oldStr, newStr),
	}
	got, err := repo.AppendEvent(context.Background(), rec)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	return got
}

// hashObjectFakeGit returns deterministic, content-derived HashObject values so
// File Blob Refs are stable across rebuilds. DiffNameOnly is unused on the
// projection cold path.
type hashObjectFakeGit struct {
	graph.GraphGitClient
	blobs map[string]string
}

func (g *hashObjectFakeGit) HashObject(_ context.Context, filePath string) (string, error) {
	if g.blobs != nil {
		if h, ok := g.blobs[filePath]; ok {
			return h, nil
		}
	}
	return "blob-" + filePath, nil
}

// dumpProjections returns a deterministic, ordered byte dump of every Projection
// table so two rebuilds can be compared for byte-identical output.
func dumpProjections(t *testing.T, repo *infragraph.SQLiteRepository) []byte {
	t.Helper()
	ctx := context.Background()
	db := repo.Client().DB()
	var b strings.Builder

	dump := func(table, query string, cols int) {
		b.WriteString("== " + table + " ==\n")
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			t.Fatalf("dump %s: %v", table, err)
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			cells := make([]any, cols)
			ptrs := make([]any, cols)
			for i := range cells {
				ptrs[i] = &cells[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("scan %s: %v", table, err)
			}
			parts := make([]string, cols)
			for i, c := range cells {
				parts[i] = fmt.Sprintf("%v", c)
			}
			lines = append(lines, strings.Join(parts, "|"))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iter %s: %v", table, err)
		}
		sort.Strings(lines)
		for _, l := range lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}

	dump("sessions", `SELECT id, source, instance_id, started_at, ended_at FROM sessions`, 5)
	dump("actions", `SELECT id, session_id, sequence, tool, files_changed, timestamp FROM actions`, 6)
	dump("action_modifies", `SELECT action_id, file_path, additions, deletions FROM action_modifies`, 4)
	dump("event_files", `SELECT event_seq, file_path, before_blob, after_blob, change_kind FROM event_files`, 5)
	return []byte(b.String())
}

func countRows(t *testing.T, repo *infragraph.SQLiteRepository, table string) int {
	t.Helper()
	var n int
	if err := repo.Client().DB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestProjectionRebuilder_DeterministicRebuild(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	// Ten chained Events alternating between two instance_ids (two sessions),
	// all within the session-timeout gap so each instance stays one session.
	for i := int64(0); i < 10; i++ {
		instance := "agent-A"
		if i%2 == 1 {
			instance = "agent-B"
		}
		seededEvent(t, repo, graph.EventSourceClaudeCode, instance, 1000+i*10,
			fmt.Sprintf("file%d.go", i), "old", "new")
	}

	rb := application.NewProjectionRebuilder(repo, &hashObjectFakeGit{})

	if err := rb.Rebuild(ctx); err != nil {
		t.Fatalf("first Rebuild: %v", err)
	}
	first := dumpProjections(t, repo)

	if got := countRows(t, repo, "sessions"); got != 2 {
		t.Errorf("sessions count = %d, want 2", got)
	}
	if got := countRows(t, repo, "actions"); got != 10 {
		t.Errorf("actions count = %d, want 10", got)
	}

	if err := rb.Rebuild(ctx); err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	second := dumpProjections(t, repo)

	if string(first) != string(second) {
		t.Errorf("rebuild not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestProjectionRebuilder_ConcurrentInstancesSplitSessions(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	// Interleave A, B, A, B on the single shared chain (one continuous
	// prev_hash/this_hash sequence, no fork), all within the timeout gap.
	instances := []string{"agent-A", "agent-B", "agent-A", "agent-B"}
	for i, inst := range instances {
		seededEvent(t, repo, graph.EventSourceClaudeCode, inst, 2000+int64(i)*10,
			fmt.Sprintf("f%d.go", i), "old", "new")
	}

	rb := application.NewProjectionRebuilder(repo, &hashObjectFakeGit{})
	if err := rb.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// Each instance_id maps to its own session.
	rows, err := repo.Client().DB().QueryContext(ctx,
		`SELECT instance_id, COUNT(*) FROM sessions GROUP BY instance_id`)
	if err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	defer rows.Close()
	perInstance := map[string]int{}
	for rows.Next() {
		var inst string
		var n int
		if err := rows.Scan(&inst, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		perInstance[inst] = n
	}
	if perInstance["agent-A"] != 1 || perInstance["agent-B"] != 1 {
		t.Errorf("sessions per instance = %v, want one each for agent-A/agent-B", perInstance)
	}

	// The chain stayed a single unforked sequence.
	res, err := repo.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("VerifyChain status = %q, want ok", res.Status)
	}
}

func TestProjectionRebuilder_RefusesOnBrokenChain(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	for i := int64(0); i < 3; i++ {
		seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 3000+i*10,
			fmt.Sprintf("b%d.go", i), "old", "new")
	}

	rb := application.NewProjectionRebuilder(repo, &hashObjectFakeGit{})
	// First build the projections so we can prove a refused rebuild leaves them
	// untouched.
	if err := rb.Rebuild(ctx); err != nil {
		t.Fatalf("initial Rebuild: %v", err)
	}
	before := dumpProjections(t, repo)

	// Tamper: edit payload_raw of the second Event without re-chaining.
	if _, err := repo.Client().DB().ExecContext(ctx,
		`UPDATE events SET payload_raw = ? WHERE seq = 2`, `{"tampered":true}`,
	); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	err := rb.Rebuild(ctx)
	if err == nil {
		t.Fatal("Rebuild on broken chain returned nil error, want refusal")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("error %q does not report the broken seq (2)", err.Error())
	}

	after := dumpProjections(t, repo)
	if string(before) != string(after) {
		t.Errorf("refused rebuild mutated projections:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestProjectionRebuilder_OnlyLastEditPerPathGetsWorkingTreeBlob(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1000, "a.go", "old", "new")
	seededEvent(t, repo, graph.EventSourceClaudeCode, "agent-A", 1010, "a.go", "new", "newer")

	// HashObject only reflects the current working tree, so the rebuild must call
	// it exactly once — for the last edit of a.go — and leave the earlier edit's
	// after_blob unknown rather than fabricating the current blob as historical.
	gitFake := &seqBlobGit{blobs: []string{"blob-current"}}
	rb := application.NewProjectionRebuilder(repo, gitFake)
	if err := rb.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if _, after := eventFileBlobsAtSeq(t, repo, 1, "a.go"); after != "" {
		t.Errorf("first edit after_blob = %q, want empty (intermediate state is unknown)", after)
	}
	before, after := eventFileBlobsAtSeq(t, repo, 2, "a.go")
	if after != "blob-current" {
		t.Errorf("last edit after_blob = %q, want blob-current (working tree)", after)
	}
	if before != "" {
		t.Errorf("last edit before_blob = %q, want empty (prior intermediate unknown)", before)
	}
	if gitFake.call != 1 {
		t.Errorf("HashObject called %d times, want 1 (only the last edit)", gitFake.call)
	}
}

// seqBlobGit returns HashObject results in call order so a single Rebuild can
// assign distinct after_blobs to successive edits on the same path.
type seqBlobGit struct {
	graph.GraphGitClient
	blobs []string
	call  int
}

func (g *seqBlobGit) HashObject(_ context.Context, _ string) (string, error) {
	if g.call >= len(g.blobs) {
		return fmt.Sprintf("blob-fallback-%d", g.call), nil
	}
	h := g.blobs[g.call]
	g.call++
	return h, nil
}

func TestProjectionRebuilder_OutOfBandEventFiles(t *testing.T) {
	ctx := context.Background()
	repo := newProjectionTestRepo(t)

	const beforeBlob = "blob-before"
	const afterBlob = "blob-after"
	rec := graph.EventRecord{
		EventID:    "oob-test",
		RecordedAt: 5000,
		Source:     graph.EventSourceUnknown,
		Kind:       graph.EventKindOutOfBand,
		ToolName:   "external-edit",
		PayloadRaw: []byte(`{"out_of_band":{"file_path":"x.go","before_blob":"blob-before","after_blob":"blob-after"}}`),
	}
	got, err := repo.AppendEvent(ctx, rec)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	rb := application.NewProjectionRebuilder(repo, &hashObjectFakeGit{})
	if err := rb.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	before, after := eventFileBlobsAtSeq(t, repo, got.Seq, "x.go")
	if before != beforeBlob {
		t.Errorf("before_blob = %q, want %q", before, beforeBlob)
	}
	if after != afterBlob {
		t.Errorf("after_blob = %q, want %q", after, afterBlob)
	}
}

func eventFileBlobsAtSeq(t *testing.T, repo *infragraph.SQLiteRepository, seq int64, path string) (string, string) {
	t.Helper()
	var before, after string
	err := repo.Client().DB().QueryRowContext(context.Background(),
		`SELECT COALESCE(before_blob,''), COALESCE(after_blob,'') FROM event_files WHERE event_seq = ? AND file_path = ?`,
		seq, path,
	).Scan(&before, &after)
	if err != nil {
		t.Fatalf("event_files for seq=%d path=%s: %v", seq, path, err)
	}
	return before, after
}
