package application_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infragraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// fakeGraphRepository records AppendEvent calls and serves a programmable
// HeadHash. It embeds graph.GraphRepository so it satisfies the full interface;
// only the Event Log methods the hot path uses are implemented. busyErr, when
// set, makes AppendEvent simulate SQLITE_BUSY lock contention. AppendEvent
// mirrors the real repository: it assigns seq, sets prev_hash from the current
// head, and computes this_hash itself — never trusting caller-supplied hashes.
type fakeGraphRepository struct {
	graph.GraphRepository
	head     string
	appended []graph.EventRecord
	nextSeq  int64
	busyErr  error
}

func (f *fakeGraphRepository) HeadHash(_ context.Context) (string, error) {
	if f.head == "" {
		return graph.GenesisHash, nil
	}
	return f.head, nil
}

func (f *fakeGraphRepository) AppendEvent(_ context.Context, e graph.EventRecord) (graph.EventRecord, error) {
	if f.busyErr != nil {
		return graph.EventRecord{}, f.busyErr
	}
	f.nextSeq++
	e.Seq = f.nextSeq
	prev := f.head
	if prev == "" {
		prev = graph.GenesisHash
	}
	e.PrevHash = prev
	e.ThisHash = fakeEventHasher{}.Hash(e.PrevHash, e)
	f.appended = append(f.appended, e)
	f.head = e.ThisHash
	return e, nil
}

// fakeEventHasher is a deterministic stand-in for the SHA-256 hasher so the
// prev/this linkage is assertable without hashing details.
type fakeEventHasher struct{}

func (fakeEventHasher) Hash(prevHash string, e graph.EventRecord) string {
	return fmt.Sprintf("h(%s|%s|%s)", prevHash, e.ToolName, string(e.PayloadRaw))
}

// failGitClient fails every call so the test proves the hot path never touches
// the git client.
type failGitClient struct{ t *testing.T }

func (g failGitClient) DiffNameOnly(context.Context) ([]string, error) {
	g.t.Fatal("hot path must not call DiffNameOnly")
	return nil, nil
}

func (g failGitClient) HashObject(context.Context, string) (string, error) {
	g.t.Fatal("hot path must not call HashObject")
	return "", nil
}

func (g failGitClient) DiffForFiles(context.Context, []string) (string, error) {
	g.t.Fatal("hot path must not call DiffForFiles")
	return "", nil
}

func (g failGitClient) CommitLogDetailed(context.Context, string, int) ([]graph.CommitInfo, error) {
	g.t.Fatal("hot path must not call CommitLogDetailed")
	return nil, nil
}

func (g failGitClient) CurrentHead(context.Context) (string, error) {
	g.t.Fatal("hot path must not call CurrentHead")
	return "", nil
}

func (g failGitClient) MergeBaseIsAncestor(context.Context, string, string) (bool, error) {
	g.t.Fatal("hot path must not call MergeBaseIsAncestor")
	return false, nil
}

func newCaptureSvc(repo graph.GraphRepository, git graph.GraphGitClient) *application.CaptureService {
	return application.NewCaptureService(repo, git, infragraph.NewUUIDSessionIDGenerator())
}

func TestCaptureService_AppendsObservedEventVerbatim(t *testing.T) {
	repo := &fakeGraphRepository{}
	svc := newCaptureSvc(repo, failGitClient{t})

	payload := []byte(`{"tool_name":"Edit","file_path":"src/main.go"}`)
	res, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source:     "claude-code",
		Tool:       "Edit",
		InstanceID: "claude-1",
		Event: &graph.EventRecord{
			Source:     graph.EventSourceClaudeCode,
			InstanceID: "claude-1",
			Kind:       graph.EventKindTool,
			ToolName:   "Edit",
			PayloadRaw: payload,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected append, got skipped: %q", res.Reason)
	}
	if len(repo.appended) != 1 {
		t.Fatalf("expected exactly one AppendEvent, got %d", len(repo.appended))
	}

	got := repo.appended[0]
	if got.Source != graph.EventSourceClaudeCode {
		t.Errorf("Source = %q, want claude-code", got.Source)
	}
	if got.Kind != graph.EventKindTool {
		t.Errorf("Kind = %q, want tool", got.Kind)
	}
	if got.ToolName != "Edit" {
		t.Errorf("ToolName = %q, want Edit", got.ToolName)
	}
	if !bytes.Equal(got.PayloadRaw, payload) {
		t.Errorf("PayloadRaw = %q, want %q", got.PayloadRaw, payload)
	}
	if got.PrevHash != graph.GenesisHash {
		t.Errorf("PrevHash = %q, want genesis", got.PrevHash)
	}
	wantThis := fakeEventHasher{}.Hash(graph.GenesisHash, got)
	if got.ThisHash != wantThis {
		t.Errorf("ThisHash = %q, want %q", got.ThisHash, wantThis)
	}
	if got.EventID == "" {
		t.Error("EventID must be assigned")
	}
	if got.RecordedAt == 0 {
		t.Error("RecordedAt must be set")
	}
}

func TestCaptureService_EditThenRevertProducesTwoEvents(t *testing.T) {
	repo := &fakeGraphRepository{}
	svc := newCaptureSvc(repo, failGitClient{t})
	ctx := context.Background()

	capture := func(payload string) {
		res, err := svc.Capture(ctx, graph.CaptureRequest{
			Source:     "claude-code",
			Tool:       "Edit",
			InstanceID: "claude-1",
			Event: &graph.EventRecord{
				Source:     graph.EventSourceClaudeCode,
				InstanceID: "claude-1",
				Kind:       graph.EventKindTool,
				ToolName:   "Edit",
				PayloadRaw: []byte(payload),
			},
		})
		if err != nil {
			t.Fatalf("capture error: %v", err)
		}
		if res.Skipped {
			t.Fatalf("capture must not be skipped: %q", res.Reason)
		}
	}

	capture(`{"file_path":"a.go","old_string":"X","new_string":"Y"}`)
	capture(`{"file_path":"a.go","old_string":"Y","new_string":"X"}`)

	if len(repo.appended) != 2 {
		t.Fatalf("edit-then-revert must produce two events, got %d", len(repo.appended))
	}
	if repo.appended[1].PrevHash != repo.appended[0].ThisHash {
		t.Errorf("second event prev_hash %q must equal first this_hash %q",
			repo.appended[1].PrevHash, repo.appended[0].ThisHash)
	}
}

func TestCaptureService_LockContentionIsNonBlocking(t *testing.T) {
	repo := &fakeGraphRepository{busyErr: graph.ErrChainBusy}
	svc := newCaptureSvc(repo, failGitClient{t})

	res, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source:     "claude-code",
		Tool:       "Edit",
		InstanceID: "claude-1",
		Event: &graph.EventRecord{
			Source:     graph.EventSourceClaudeCode,
			InstanceID: "claude-1",
			Kind:       graph.EventKindTool,
			ToolName:   "Edit",
			PayloadRaw: []byte(`{"file_path":"a.go"}`),
		},
	})
	if err != nil {
		t.Fatalf("lock contention must not return an error, got: %v", err)
	}
	if res == nil || !res.Skipped {
		t.Fatal("expected a skipped result on lock contention")
	}
	if len(repo.appended) != 0 {
		t.Errorf("no event should be appended on contention, got %d", len(repo.appended))
	}
}

func TestCaptureService_EndSessionAppendsNoEvent(t *testing.T) {
	repo := &fakeGraphRepository{}
	svc := newCaptureSvc(repo, failGitClient{t})

	res, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source:     "claude-code",
		InstanceID: "claude-1",
		EndSession: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || !res.Skipped {
		t.Fatal("end-session capture should be a skipped result")
	}
	if len(repo.appended) != 0 {
		t.Errorf("end-session must not append an event, got %d", len(repo.appended))
	}
}
