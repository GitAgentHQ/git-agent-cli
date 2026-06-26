package cmd

import (
	"bytes"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	"github.com/gitagenthq/git-agent/infrastructure/redact"
)

func TestMergeHookPayload(t *testing.T) {
	tests := []struct {
		name           string
		tool, instance string
		stdin          string
		wantTool       string
		wantInstance   string
	}{
		{
			name:         "fills tool and instance from payload when flags empty",
			stdin:        `{"tool_name":"Edit","session_id":"claude-abc"}`,
			wantTool:     "Edit",
			wantInstance: "claude-abc",
		},
		{
			name:         "explicit tool flag overrides payload",
			tool:         "Write",
			stdin:        `{"tool_name":"Edit","session_id":"claude-abc"}`,
			wantTool:     "Write",
			wantInstance: "claude-abc",
		},
		{
			name:         "explicit instance flag overrides payload",
			instance:     "manual-1",
			stdin:        `{"tool_name":"Edit","session_id":"claude-abc"}`,
			wantTool:     "Edit",
			wantInstance: "manual-1",
		},
		{
			name:         "empty stdin leaves flags untouched",
			tool:         "Bash",
			instance:     "x",
			stdin:        "",
			wantTool:     "Bash",
			wantInstance: "x",
		},
		{
			name:         "malformed payload is ignored, never errors",
			stdin:        `not json`,
			wantTool:     "",
			wantInstance: "",
		},
		{
			name:         "partial payload fills only present fields",
			stdin:        `{"tool_name":"Read"}`,
			wantTool:     "Read",
			wantInstance: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTool, gotInstance := mergeHookPayload(tt.tool, tt.instance, []byte(tt.stdin))
			if gotTool != tt.wantTool || gotInstance != tt.wantInstance {
				t.Errorf("mergeHookPayload(%q,%q,%q) = (%q,%q), want (%q,%q)",
					tt.tool, tt.instance, tt.stdin, gotTool, gotInstance, tt.wantTool, tt.wantInstance)
			}
		})
	}
}

func TestBuildEventRecord_FullPayloadMapping(t *testing.T) {
	stdin := []byte(`{
		"session_id": "claude-xyz",
		"transcript_path": "/tmp/transcript.jsonl",
		"cwd": "/home/me/proj",
		"hook_event_name": "PostToolUse",
		"permission_mode": "acceptEdits",
		"tool_name": "Edit",
		"tool_input": {"file_path": "src/main.go", "old_string": "a", "new_string": "b"},
		"tool_response": {"success": true}
	}`)

	rec, _, ok := buildEventRecord("claude-code", "", "", stdin, redact.NewRedactor())
	if !ok {
		t.Fatal("expected ok=true for a well-formed PostToolUse payload")
	}

	if rec.Source != graph.EventSourceClaudeCode {
		t.Errorf("Source = %q, want %q", rec.Source, graph.EventSourceClaudeCode)
	}
	if rec.Kind != graph.EventKindTool {
		t.Errorf("Kind = %q, want %q", rec.Kind, graph.EventKindTool)
	}
	if rec.ToolName != "Edit" {
		t.Errorf("ToolName = %q, want Edit", rec.ToolName)
	}
	if rec.InstanceID != "claude-xyz" {
		t.Errorf("InstanceID = %q, want claude-xyz", rec.InstanceID)
	}
	if rec.TranscriptPath != "/tmp/transcript.jsonl" {
		t.Errorf("TranscriptPath = %q", rec.TranscriptPath)
	}
	if rec.Cwd != "/home/me/proj" {
		t.Errorf("Cwd = %q", rec.Cwd)
	}
	if rec.HookEventName != "PostToolUse" {
		t.Errorf("HookEventName = %q", rec.HookEventName)
	}
	if rec.PermissionMode != "acceptEdits" {
		t.Errorf("PermissionMode = %q", rec.PermissionMode)
	}
	if len(rec.PayloadRaw) == 0 {
		t.Fatal("PayloadRaw must carry the post-redaction bytes, got empty")
	}
}

func TestBuildEventRecord_ExplicitArgsOverridePayload(t *testing.T) {
	stdin := []byte(`{"tool_name":"Edit","session_id":"from-payload"}`)

	rec, _, ok := buildEventRecord("claude-code", "Write", "from-flag", stdin, redact.NewRedactor())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if rec.ToolName != "Write" {
		t.Errorf("explicit tool should win: ToolName = %q, want Write", rec.ToolName)
	}
	if rec.InstanceID != "from-flag" {
		t.Errorf("explicit instance should win: InstanceID = %q, want from-flag", rec.InstanceID)
	}
}

func TestBuildEventRecord_PayloadRawIsPostRedaction(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	stdin := []byte(`{"tool_name":"Bash","session_id":"s","tool_input":{"command":"aws --key ` + secret + `"}}`)

	rec, _, ok := buildEventRecord("claude-code", "", "", stdin, redact.NewRedactor())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if bytes.Contains(rec.PayloadRaw, []byte(secret)) {
		t.Fatalf("raw secret leaked into PayloadRaw: %q", rec.PayloadRaw)
	}
	if !bytes.Contains(rec.PayloadRaw, []byte("[REDACTED:aws-access-token]")) {
		t.Errorf("expected redaction placeholder in PayloadRaw, got %q", rec.PayloadRaw)
	}
}

func TestBuildEventRecord_MalformedJSONIsNoOp(t *testing.T) {
	_, _, ok := buildEventRecord("claude-code", "", "", []byte("{not json"), redact.NewRedactor())
	if ok {
		t.Error("malformed JSON must yield ok=false (no EventRecord, no error)")
	}
}

func TestBuildEventRecord_InteractiveNoStdinIsNoOp(t *testing.T) {
	_, _, ok := buildEventRecord("claude-code", "", "", nil, redact.NewRedactor())
	if ok {
		t.Error("interactive (no piped stdin) must yield ok=false")
	}
}

func TestBuildEventRecord_RecordsTruncationMetadata(t *testing.T) {
	var sb bytes.Buffer
	sb.WriteString(`{"tool_name":"Write","session_id":"s","tool_input":{"content":"`)
	sb.Write(bytes.Repeat([]byte("x"), 600*1024))
	sb.WriteString(`"}}`)
	stdin := sb.Bytes()

	rec, _, ok := buildEventRecord("claude-code", "", "", stdin, redact.NewRedactor())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !rec.Truncated {
		t.Error("expected Truncated=true for oversized payload")
	}
	if rec.PayloadSize != int64(len(stdin)) {
		t.Errorf("PayloadSize = %d, want original size %d", rec.PayloadSize, len(stdin))
	}
	if int64(len(rec.PayloadRaw)) >= rec.PayloadSize {
		t.Errorf("PayloadRaw (%d) should be smaller than original size (%d)", len(rec.PayloadRaw), rec.PayloadSize)
	}
}
