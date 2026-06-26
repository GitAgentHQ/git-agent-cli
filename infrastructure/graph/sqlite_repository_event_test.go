package graph

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	domaingraph "github.com/gitagenthq/git-agent/domain/graph"
)

func newEventTestRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	ctx := context.Background()
	client := NewSQLiteClient(filepath.Join(t.TempDir(), "events.db"))
	repo := NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return repo
}

func toolEvent(id string, recordedAt int64) domaingraph.EventRecord {
	return domaingraph.EventRecord{
		EventID:    id,
		RecordedAt: recordedAt,
		Source:     domaingraph.EventSourceClaudeCode,
		InstanceID: "agent-1",
		Kind:       domaingraph.EventKindTool,
		ToolName:   "Edit",
		PayloadRaw: []byte(`{"tool_input":{"file_path":"a.go"}}`),
	}
}

func TestAppendEvent_FirstEventFromGenesis(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)

	got, err := repo.AppendEvent(ctx, toolEvent("evt-1", 100))
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if got.Seq != 1 {
		t.Errorf("first event Seq = %d, want 1", got.Seq)
	}
	if got.PrevHash != domaingraph.GenesisHash {
		t.Errorf("first event PrevHash = %q, want Genesis %q", got.PrevHash, domaingraph.GenesisHash)
	}
	if len(got.ThisHash) != 64 {
		t.Errorf("ThisHash = %q, want 64-char hex", got.ThisHash)
	}
}

func TestAppendEvent_ChainsPrevToPriorThisHash(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)

	var prev domaingraph.EventRecord
	for i := int64(1); i <= 3; i++ {
		got, err := repo.AppendEvent(ctx, toolEvent(fmt.Sprintf("evt-%d", 100+i), 100+i))
		if err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
		if got.Seq != i {
			t.Errorf("event %d Seq = %d, want %d", i, got.Seq, i)
		}
		if i == 1 {
			if got.PrevHash != domaingraph.GenesisHash {
				t.Errorf("event 1 PrevHash = %q, want Genesis", got.PrevHash)
			}
		} else if got.PrevHash != prev.ThisHash {
			t.Errorf("event %d PrevHash = %q, want prior ThisHash %q", i, got.PrevHash, prev.ThisHash)
		}
		prev = got
	}
}

func TestHeadHash(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)

	head, err := repo.HeadHash(ctx)
	if err != nil {
		t.Fatalf("HeadHash (empty): %v", err)
	}
	if head != domaingraph.GenesisHash {
		t.Errorf("empty-log HeadHash = %q, want Genesis %q", head, domaingraph.GenesisHash)
	}

	var last domaingraph.EventRecord
	for i := int64(1); i <= 3; i++ {
		last, err = repo.AppendEvent(ctx, toolEvent(fmt.Sprintf("evt-%d", 200+i), 200+i))
		if err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
	}

	head, err = repo.HeadHash(ctx)
	if err != nil {
		t.Fatalf("HeadHash: %v", err)
	}
	if head != last.ThisHash {
		t.Errorf("HeadHash = %q, want last ThisHash %q", head, last.ThisHash)
	}
}

func TestStreamEvents_SinceSeq(t *testing.T) {
	ctx := context.Background()
	repo := newEventTestRepo(t)

	for i := int64(1); i <= 3; i++ {
		if _, err := repo.AppendEvent(ctx, toolEvent(fmt.Sprintf("evt-%d", 300+i), 300+i)); err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
	}

	cur, err := repo.StreamEvents(ctx, 1)
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	defer cur.Close()

	var seqs []int64
	for cur.Next() {
		seqs = append(seqs, cur.Event().Seq)
	}
	if err := cur.Err(); err != nil {
		t.Fatalf("cursor err: %v", err)
	}
	if len(seqs) != 2 || seqs[0] != 2 || seqs[1] != 3 {
		t.Errorf("StreamEvents(sinceSeq=1) seqs = %v, want [2 3]", seqs)
	}
}
