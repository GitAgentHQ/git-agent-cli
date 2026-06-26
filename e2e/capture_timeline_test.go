package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// TestDiagnose_EmptyLogReportsNoCandidates locks the real diagnose contract on a
// repo with an empty Event Log: the chain verifies clean, no failing Outcome
// Event exists, so diagnose reports zero candidates and exits 0.
func TestDiagnose_EmptyLogReportsNoCandidates(t *testing.T) {
	dir := newGitRepo(t)
	stdout, stderr, code := gitAgentSeparated(t, dir, "graph", "diagnose", "test bug", "--text")
	if code != 0 {
		t.Fatalf("diagnose: exit code %d (want 0)\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "0 candidates") {
		t.Errorf("diagnose stdout missing the no-candidates report\ngot: %s\nstderr: %s", stdout, stderr)
	}
}

// TestCapture_HiddenFromHelp locks the command visibility contract: capture
// is an internal hook target and must stay hidden, while the graph read
// commands (timeline, diagnose) are user-facing under the `graph` parent.
func TestCapture_HiddenFromHelp(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "--help")
	if code != 0 {
		t.Fatalf("--help: exit code %d\noutput: %s", code, out)
	}
	if strings.Contains(out, "capture") {
		t.Errorf("capture must be hidden from --help\noutput: %s", out)
	}
	if !strings.Contains(out, "graph") {
		t.Errorf("graph parent missing from --help\noutput: %s", out)
	}
	// timeline and diagnose moved under `graph`; they must not appear at the
	// top level, only under `git-agent graph --help`.
	if strings.Contains(out, "timeline") || strings.Contains(out, "diagnose") {
		t.Errorf("timeline/diagnose must not appear at top-level --help (now under graph)\noutput: %s", out)
	}
	graphHelp, code := gitAgent(t, dir, "graph", "--help")
	if code != 0 {
		t.Fatalf("graph --help: exit code %d\noutput: %s", code, graphHelp)
	}
	if !strings.Contains(graphHelp, "timeline") {
		t.Errorf("timeline missing from graph --help\noutput: %s", graphHelp)
	}
	if !strings.Contains(graphHelp, "diagnose") {
		t.Errorf("diagnose missing from graph --help\noutput: %s", graphHelp)
	}
}

// TestCapture_AppendsObservedPayload exercises the append-only hot path
// end-to-end: a PostToolUse payload on stdin is appended verbatim as one Event,
// the hook exits 0, and a second payload chains onto the first. Timeline is now
// a cold projection (built by `graph index`, later task) and is intentionally
// not asserted here.
func TestCapture_AppendsObservedPayload(t *testing.T) {
	dir := newGitRepo(t)

	payload1 := []byte(`{"session_id":"claude-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Edit","tool_input":{"file_path":"src/a.go","old_string":"x","new_string":"y"}}`)
	out, code := gitAgentStdin(t, dir, payload1, "capture", "--source", "claude-code")
	if code != 0 {
		t.Fatalf("capture #1: exit code %d\noutput: %s", code, out)
	}
	var res1 graph.CaptureResult
	if err := json.Unmarshal([]byte(out), &res1); err != nil {
		t.Fatalf("capture #1 output not JSON: %v\n%s", err, out)
	}
	if res1.Skipped {
		t.Errorf("capture #1: unexpectedly skipped (%s)", res1.Reason)
	}
	if res1.EventID == "" {
		t.Errorf("capture #1: event_id empty\noutput: %s", out)
	}
	if res1.Seq != 1 {
		t.Errorf("capture #1: seq = %d (want 1)", res1.Seq)
	}
	if res1.Source != "claude-code" {
		t.Errorf("capture #1: source %q (want claude-code)", res1.Source)
	}

	payload2 := []byte(`{"session_id":"claude-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Edit","tool_input":{"file_path":"src/a.go","old_string":"y","new_string":"x"}}`)
	out, code = gitAgentStdin(t, dir, payload2, "capture", "--source", "claude-code")
	if code != 0 {
		t.Fatalf("capture #2: exit code %d\noutput: %s", code, out)
	}
	var res2 graph.CaptureResult
	if err := json.Unmarshal([]byte(out), &res2); err != nil {
		t.Fatalf("capture #2 output not JSON: %v\n%s", err, out)
	}
	// Edit-then-revert is preserved as two distinct Events, never a net no-op.
	if res2.Seq != 2 {
		t.Errorf("capture #2: seq = %d (want 2, edit-then-revert is two events)", res2.Seq)
	}
	if res2.EventID == res1.EventID {
		t.Error("capture #2: event_id must differ from the first event")
	}
}

// TestGraphStatus_ReportsProjectionCounts locks the read-only status contract:
// after capturing two events and running `graph index`, `graph status` reports the session
// and action counts that the projections now hold.
func TestGraphStatus_ReportsProjectionCounts(t *testing.T) {
	dir := newGitRepo(t)

	payload1 := []byte(`{"session_id":"sess-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Edit","tool_input":{"file_path":"src/a.go","old_string":"x","new_string":"y"}}`)
	if out, code := gitAgentStdin(t, dir, payload1, "capture", "--source", "claude-code"); code != 0 {
		t.Fatalf("capture #1: exit %d\n%s", code, out)
	}
	payload2 := []byte(`{"session_id":"sess-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Write","tool_input":{"file_path":"src/b.go","content":"package main\n"}}`)
	if out, code := gitAgentStdin(t, dir, payload2, "capture", "--source", "claude-code"); code != 0 {
		t.Fatalf("capture #2: exit %d\n%s", code, out)
	}
	if out, code := gitAgent(t, dir, "graph", "index"); code != 0 {
		t.Fatalf("graph index: exit %d\n%s", code, out)
	}

	out, code := gitAgent(t, dir, "graph", "status", "--json")
	if code != 0 {
		t.Fatalf("graph status: exit %d\n%s", code, out)
	}
	var stats graph.GraphStats
	if err := json.Unmarshal([]byte(out), &stats); err != nil {
		t.Fatalf("graph status output not JSON: %v\n%s", err, out)
	}
	if !stats.Exists {
		t.Errorf("stats.Exists = false, want true")
	}
	if stats.SessionCount != 1 {
		t.Errorf("SessionCount = %d, want 1\n%s", stats.SessionCount, out)
	}
	if stats.ActionCount != 2 {
		t.Errorf("ActionCount = %d, want 2\n%s", stats.ActionCount, out)
	}
}

// TestCapture_RebuildReflectsTimeline restores the timeline coverage carried
// forward from the append-only rewrite: capture two PostToolUse Events, run
// `graph index` to replay the Event Log into the projections, then assert the
// timeline reflects one session with both captured actions.
func TestCapture_RebuildReflectsTimeline(t *testing.T) {
	dir := newGitRepo(t)

	payload1 := []byte(`{"session_id":"sess-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Edit","tool_input":{"file_path":"src/a.go","old_string":"x","new_string":"y"}}`)
	if out, code := gitAgentStdin(t, dir, payload1, "capture", "--source", "claude-code"); code != 0 {
		t.Fatalf("capture #1: exit %d\n%s", code, out)
	}
	payload2 := []byte(`{"session_id":"sess-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Write","tool_input":{"file_path":"src/b.go","content":"package main\n"}}`)
	if out, code := gitAgentStdin(t, dir, payload2, "capture", "--source", "claude-code"); code != 0 {
		t.Fatalf("capture #2: exit %d\n%s", code, out)
	}

	if out, code := gitAgent(t, dir, "graph", "index"); code != 0 {
		t.Fatalf("graph index: exit %d\n%s", code, out)
	}

	out, code := gitAgent(t, dir, "graph", "timeline", "--json")
	if code != 0 {
		t.Fatalf("timeline: exit %d\n%s", code, out)
	}
	var result graph.TimelineResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("timeline output not JSON: %v\n%s", err, out)
	}
	if result.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1\n%s", result.TotalSessions, out)
	}
	if result.TotalActions != 2 {
		t.Errorf("TotalActions = %d, want 2 (one per captured Event)\n%s", result.TotalActions, out)
	}
	if len(result.Sessions) == 1 && result.Sessions[0].Source != "claude-code" {
		t.Errorf("session source = %q, want claude-code", result.Sessions[0].Source)
	}
}

// TestCapture_NoPayloadIsNonBlockingNoOp locks the degraded contract: when no
// hook payload is piped (interactive/flag-only), capture appends nothing, exits
// 0, and never errors the agent.
func TestCapture_NoPayloadIsNonBlockingNoOp(t *testing.T) {
	dir := newGitRepo(t)

	out, code := gitAgent(t, dir, "capture", "--source", "claude-code", "--tool", "Edit")
	if code != 0 {
		t.Fatalf("flag-only capture: exit code %d\noutput: %s", code, out)
	}
	var res graph.CaptureResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("flag-only capture output not JSON: %v\n%s", err, out)
	}
	if !res.Skipped {
		t.Errorf("flag-only capture must be a no-op skip\noutput: %s", out)
	}
}

// TestCapture_EndSessionIsNonBlocking locks the --end-session contract under the
// Event Log redesign: it exits 0 and appends no Event (session boundaries are
// derived on the cold projection path from inter-Event gaps).
func TestCapture_EndSessionIsNonBlocking(t *testing.T) {
	dir := newGitRepo(t)

	out, code := gitAgent(t, dir, "capture", "--source", "claude-code", "--end-session")
	if code != 0 {
		t.Fatalf("end-session: exit code %d\noutput: %s", code, out)
	}
	var res graph.CaptureResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("end-session output not JSON: %v\n%s", err, out)
	}
	if !res.Skipped {
		t.Errorf("end-session must be a skip result\noutput: %s", out)
	}
}

// TestCapture_AbsolutePathNormalizedForFileFilter locks the path-normalization
// contract: a PostToolUse payload carrying an ABSOLUTE file_path (as Claude Code
// does) must be stored repo-relative so `graph timeline --file <relative>` and
// `graph provenance <relative>` match it — and so captured rows join the same
// path key as out-of-band reconciled rows.
func TestCapture_AbsolutePathNormalizedForFileFilter(t *testing.T) {
	dir := newGitRepo(t)

	absPath := dir + "/src/a.go"
	payload := []byte(`{"session_id":"sess-abs","hook_event_name":"PostToolUse",` +
		`"tool_name":"Edit","cwd":"` + dir + `",` +
		`"tool_input":{"file_path":"` + absPath + `","old_string":"x","new_string":"y"}}`)
	if out, code := gitAgentStdin(t, dir, payload, "capture", "--source", "claude-code"); code != 0 {
		t.Fatalf("capture: exit %d\n%s", code, out)
	}
	if out, code := gitAgent(t, dir, "graph", "index"); code != 0 {
		t.Fatalf("graph index: exit %d\n%s", code, out)
	}

	// Relative path filter must match the captured row despite the payload's
	// absolute file_path.
	out, code := gitAgent(t, dir, "graph", "timeline", "--file", "src/a.go", "--json")
	if code != 0 {
		t.Fatalf("timeline --file: exit %d\n%s", code, out)
	}
	var tl graph.TimelineResult
	if err := json.Unmarshal([]byte(out), &tl); err != nil {
		t.Fatalf("timeline not JSON: %v\n%s", err, out)
	}
	if tl.TotalActions != 1 {
		t.Errorf("TotalActions = %d, want 1 (absolute file_path not normalized to relative)\n%s", tl.TotalActions, out)
	}

	// Provenance on the relative path must return the captured row.
	out, code = gitAgent(t, dir, "graph", "provenance", "src/a.go", "--json")
	if code != 0 {
		t.Fatalf("provenance: exit %d\n%s", code, out)
	}
	var prov struct {
		File string `json:"File"`
		Rows []struct {
			Who string `json:"Who"`
		} `json:"Rows"`
	}
	if err := json.Unmarshal([]byte(out), &prov); err != nil {
		t.Fatalf("provenance not JSON: %v\n%s", err, out)
	}
	if len(prov.Rows) == 0 {
		t.Errorf("provenance src/a.go returned no rows (path mismatch)\n%s", out)
	}
}
