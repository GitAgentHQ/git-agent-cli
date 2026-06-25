package graph

import (
	"context"
	"fmt"
	"testing"

	domaingraph "github.com/gitagenthq/git-agent/domain/graph"
)

// appendChain appends n valid Events through the real append path so prev_hash,
// this_hash, and seq are genuine, then returns the appended records.
func appendChain(t *testing.T, repo *SQLiteRepository, n int) []domaingraph.EventRecord {
	t.Helper()
	ctx := context.Background()
	out := make([]domaingraph.EventRecord, 0, n)
	for i := 1; i <= n; i++ {
		got, err := repo.AppendEvent(ctx, toolEvent(fmt.Sprintf("evt-%d", i), int64(1000+i)))
		if err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
		out = append(out, got)
	}
	return out
}

func TestVerifyChain_UntouchedChainIsOK(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)
	appendChain(t, repo, 3)

	res, err := repo.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("Status = %q, want ok", res.Status)
	}
	if res.FirstBreak != nil {
		t.Errorf("FirstBreak = %+v, want nil", res.FirstBreak)
	}
	if res.EventsTotal != 3 || res.EventsVerified != 3 {
		t.Errorf("EventsTotal=%d EventsVerified=%d, want 3/3", res.EventsTotal, res.EventsVerified)
	}
}

func TestVerifyChain_EditedRow(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)
	appendChain(t, repo, 3)

	if _, err := repo.Client().DB().ExecContext(ctx,
		`UPDATE events SET payload_raw = ? WHERE seq = 2`, `{"tampered":true}`,
	); err != nil {
		t.Fatalf("tamper update: %v", err)
	}

	res, err := repo.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Status != "broken" {
		t.Fatalf("Status = %q, want broken", res.Status)
	}
	if res.FirstBreak == nil {
		t.Fatal("FirstBreak = nil, want a break")
	}
	if res.FirstBreak.Seq != 2 {
		t.Errorf("FirstBreak.Seq = %d, want 2", res.FirstBreak.Seq)
	}
	if res.FirstBreak.Kind != domaingraph.ChainBreakRowEdited {
		t.Errorf("FirstBreak.Kind = %q, want ROW_EDITED", res.FirstBreak.Kind)
	}
}

func TestVerifyChain_DeletedRowIsGap(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)
	appendChain(t, repo, 5)

	if _, err := repo.Client().DB().ExecContext(ctx,
		`DELETE FROM events WHERE seq = 3`,
	); err != nil {
		t.Fatalf("tamper delete: %v", err)
	}

	res, err := repo.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Status != "broken" {
		t.Fatalf("Status = %q, want broken", res.Status)
	}
	if res.FirstBreak == nil || res.FirstBreak.Kind != domaingraph.ChainBreakRowDeleted {
		t.Errorf("FirstBreak = %+v, want ROW_DELETED", res.FirstBreak)
	}
}

func TestVerifyChain_InsertedRow(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)
	appendChain(t, repo, 3)

	// Insert an extra row whose this_hash is self-consistent but whose prev_hash
	// points nowhere reachable from the genesis linkage walk.
	if _, err := repo.Client().DB().ExecContext(ctx,
		`INSERT INTO events (
			seq, event_id, recorded_at, source, instance_id, kind,
			hook_event_name, tool_name, cwd, transcript_path, permission_mode,
			payload_raw, payload_size, truncated,
			command, exit_code, exit_code_source, is_test, is_build, test_name,
			prev_hash, this_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		4, "evt-orphan", 2000, "claude-code", "agent-1", "tool",
		"", "Edit", "", "", "",
		`{"orphan":true}`, 0, 0,
		"", nil, "", 0, 0, "",
		"deadbeef", "feedface",
	); err != nil {
		t.Fatalf("tamper insert: %v", err)
	}

	res, err := repo.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Status != "broken" {
		t.Fatalf("Status = %q, want broken", res.Status)
	}
	if res.FirstBreak == nil || res.FirstBreak.Kind != domaingraph.ChainBreakRowInserted {
		t.Errorf("FirstBreak = %+v, want ROW_INSERTED", res.FirstBreak)
	}
}

func TestVerifyChain_ReorderedRows(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)

	// Build a chain by hand where genesis linkage walks A->B->C (each prev_hash
	// points at the prior row's this_hash, and every this_hash recomputes over the
	// row's own seq+prev_hash+payload) but the seq values run A=1, C=2, B=3. Every
	// self-hash is valid; only the linkage order disagrees with seq order, which is
	// exactly ROW_REORDERED.
	hasher := NewSHA256Hasher()
	insert := func(seq int64, prev string, payload string) string {
		rec := domaingraph.EventRecord{
			Seq:        seq,
			RecordedAt: 1000 + seq,
			Source:     domaingraph.EventSourceClaudeCode,
			InstanceID: "agent-1",
			Kind:       domaingraph.EventKindTool,
			ToolName:   "Edit",
			PayloadRaw: []byte(payload),
		}
		this := hasher.Hash(prev, rec)
		if _, err := repo.Client().DB().ExecContext(ctx,
			`INSERT INTO events (
				seq, event_id, recorded_at, source, instance_id, kind,
				hook_event_name, tool_name, cwd, transcript_path, permission_mode,
				payload_raw, payload_size, truncated,
				command, exit_code, exit_code_source, is_test, is_build, test_name,
				prev_hash, this_hash
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.Seq, fmt.Sprintf("evt-%d", seq), rec.RecordedAt, string(rec.Source), rec.InstanceID, string(rec.Kind),
			"", rec.ToolName, "", "", "",
			payload, 0, 0,
			"", nil, "", 0, 0, "",
			prev, this,
		); err != nil {
			t.Fatalf("insert seq=%d: %v", seq, err)
		}
		return this
	}

	aHash := insert(1, domaingraph.GenesisHash, `{"n":"a"}`)
	// B links after A but is stored at seq 3; C links after B but is stored at seq 2.
	bHash := insert(3, aHash, `{"n":"b"}`)
	insert(2, bHash, `{"n":"c"}`)

	res, err := repo.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.Status != "broken" {
		t.Fatalf("Status = %q, want broken", res.Status)
	}
	if res.FirstBreak == nil || res.FirstBreak.Kind != domaingraph.ChainBreakRowReordered {
		t.Errorf("FirstBreak = %+v, want ROW_REORDERED", res.FirstBreak)
	}
}
