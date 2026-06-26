package application_test

import (
	"context"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// bashCapture drives CaptureService.Capture with a Bash payload and returns the
// single appended EventRecord. The git client is the strict failGitClient so the
// test proves Outcome capture never shells out on the hot path.
func bashCapture(t *testing.T, command string, toolResponse []byte) graph.EventRecord {
	t.Helper()
	repo := &fakeGraphRepository{}
	svc := newCaptureSvc(repo, failGitClient{t})

	res, err := svc.Capture(context.Background(), graph.CaptureRequest{
		Source:       "claude-code",
		Tool:         "Bash",
		InstanceID:   "claude-1",
		ToolResponse: toolResponse,
		Event: &graph.EventRecord{
			Source:     graph.EventSourceClaudeCode,
			InstanceID: "claude-1",
			Kind:       graph.EventKindTool,
			ToolName:   "Bash",
			Command:    command,
			PayloadRaw: []byte(`{"tool_name":"Bash","tool_input":{"command":"` + command + `"}}`),
		},
	})
	if err != nil {
		t.Fatalf("capture error: %v", err)
	}
	if res.Skipped {
		t.Fatalf("capture must not be skipped: %q", res.Reason)
	}
	if len(repo.appended) != 1 {
		t.Fatalf("expected exactly one appended event, got %d", len(repo.appended))
	}
	return repo.appended[0]
}

func TestCapture_FailingTestCommandIsOutcomeEvent(t *testing.T) {
	got := bashCapture(t, "go test ./...", []byte(`{"exit_code":1,"stdout":"--- FAIL"}`))

	if got.Kind != graph.EventKindOutcome {
		t.Errorf("Kind = %q, want outcome", got.Kind)
	}
	if !got.IsTest {
		t.Error("IsTest = false, want true")
	}
	if got.ExitCode == nil || *got.ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", got.ExitCode)
	}
	if got.ExitCodeSource != "reported" {
		t.Errorf("ExitCodeSource = %q, want reported", got.ExitCodeSource)
	}
	if got.PrevHash == "" || got.ThisHash == "" {
		t.Errorf("outcome event must be chained: prev=%q this=%q", got.PrevHash, got.ThisHash)
	}
}

func TestCapture_InferredExitCodeIsMarkedInferred(t *testing.T) {
	got := bashCapture(t, "make test", []byte(`{"stdout":"--- FAIL: TestX","stderr":""}`))

	if got.Kind != graph.EventKindOutcome {
		t.Errorf("Kind = %q, want outcome", got.Kind)
	}
	if got.ExitCode == nil || *got.ExitCode == 0 {
		t.Errorf("ExitCode = %v, want non-zero", got.ExitCode)
	}
	if got.ExitCodeSource != "inferred" {
		t.Errorf("ExitCodeSource = %q, want inferred", got.ExitCodeSource)
	}
}

func TestCapture_NonTestBashCommandHasNoClassification(t *testing.T) {
	got := bashCapture(t, "ls", []byte(`{"exit_code":0,"stdout":"file.txt"}`))

	if got.Kind != graph.EventKindOutcome {
		t.Errorf("Kind = %q, want outcome", got.Kind)
	}
	if got.IsTest {
		t.Error("IsTest = true, want false")
	}
	if got.IsBuild {
		t.Error("IsBuild = true, want false")
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", got.ExitCode)
	}
	if got.ExitCodeSource != "reported" {
		t.Errorf("ExitCodeSource = %q, want reported", got.ExitCodeSource)
	}
}
