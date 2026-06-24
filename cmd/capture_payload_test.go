package cmd

import "testing"

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
