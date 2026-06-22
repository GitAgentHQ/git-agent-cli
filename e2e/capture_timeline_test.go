package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// TestDiagnose_StubMessage locks the diagnose stub contract: exit 0, the
// not-yet-implemented notice on stderr, and an empty stdout. See
// docs/plans/2026-04-06-code-graph-plan/task-018-e2e-p1b-test.md.
func TestDiagnose_StubMessage(t *testing.T) {
	dir := newGitRepo(t)
	stdout, stderr, code := gitAgentSeparated(t, dir, "diagnose", "test bug")
	if code != 0 {
		t.Fatalf("diagnose: exit code %d (want 0)\nstderr: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("diagnose stdout not empty: %q", stdout)
	}
	if !strings.Contains(stderr, "not yet implemented") {
		t.Errorf("diagnose stderr missing stub message\ngot: %s", stderr)
	}
}

// TestCapture_HiddenFromHelp locks the command visibility contract: capture
// is an internal hook target and must stay hidden, while timeline and diagnose
// are user-facing.
func TestCapture_HiddenFromHelp(t *testing.T) {
	dir := newGitRepo(t)
	out, code := gitAgent(t, dir, "--help")
	if code != 0 {
		t.Fatalf("--help: exit code %d\noutput: %s", code, out)
	}
	if strings.Contains(out, "capture") {
		t.Errorf("capture must be hidden from --help\noutput: %s", out)
	}
	if !strings.Contains(out, "timeline") {
		t.Errorf("timeline missing from --help\noutput: %s", out)
	}
	if !strings.Contains(out, "diagnose") {
		t.Errorf("diagnose missing from --help\noutput: %s", out)
	}
}

// TestCapture_Timeline_System exercises the full P1b pipeline end-to-end:
// capture records agent actions into the graph, and timeline reports them.
func TestCapture_Timeline_System(t *testing.T) {
	dir := newGitRepo(t)

	// First action: edit a file then capture as a Claude Code PostToolUse
	// hook would — the change is still uncommitted in the working tree, which
	// is what capture keys off (git diff --name-only).
	writeFile(t, dir+"/src/a.go", "package src\n")
	out, code := gitAgent(t, dir, "capture", "--source", "claude-code", "--tool", "Edit")
	if code != 0 {
		t.Fatalf("capture #1: exit code %d\noutput: %s", code, out)
	}
	var res1 graph.CaptureResult
	if err := json.Unmarshal([]byte(out), &res1); err != nil {
		t.Fatalf("capture #1 output not JSON: %v\n%s", err, out)
	}
	if res1.ActionID == "" {
		t.Errorf("capture #1: action_id empty\noutput: %s", out)
	}
	if res1.Skipped {
		t.Errorf("capture #1: unexpectedly skipped (%s)", res1.Reason)
	}

	// Second action in the same session: stage the first file so it leaves the
	// working-tree diff, then edit a second file.
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "feat: add a")
	writeFile(t, dir+"/src/b.go", "package src\n")
	if out, code = gitAgent(t, dir, "capture", "--source", "claude-code", "--tool", "Edit"); code != 0 {
		t.Fatalf("capture #2: exit code %d\noutput: %s", code, out)
	}

	// Timeline should now show one session with two actions.
	out, code = gitAgent(t, dir, "timeline", "--json")
	if code != 0 {
		t.Fatalf("timeline: exit code %d\noutput: %s", code, out)
	}
	var tl graph.TimelineResult
	if err := json.Unmarshal([]byte(out), &tl); err != nil {
		t.Fatalf("timeline output not JSON: %v\n%s", err, out)
	}
	if tl.TotalSessions != 1 {
		t.Fatalf("timeline: total_sessions %d (want 1)\n%+v", tl.TotalSessions, tl)
	}
	if tl.TotalActions != 2 {
		t.Fatalf("timeline: total_actions %d (want 2)\n%+v", tl.TotalActions, tl)
	}
	if len(tl.Sessions) != 1 {
		t.Fatalf("timeline: %d sessions (want 1)\n%+v", len(tl.Sessions), tl)
	}
	s := tl.Sessions[0]
	if s.Source != "claude-code" {
		t.Errorf("timeline: session source %q (want claude-code)", s.Source)
	}
	if s.ActionCount != 2 {
		t.Errorf("timeline: action_count %d (want 2)", s.ActionCount)
	}
	if len(s.Actions) != 2 {
		t.Fatalf("timeline: %d actions (want 2)", len(s.Actions))
	}
	for i, a := range s.Actions {
		if a.Tool != "Edit" {
			t.Errorf("timeline: action[%d].tool %q (want Edit)", i, a.Tool)
		}
		if len(a.Files) == 0 {
			t.Errorf("timeline: action[%d] has no files", i)
		}
	}
}

// TestCapture_EndSessionLifecycle locks session finalization: after
// --end-session, a subsequent capture in the same instance is skipped.
func TestCapture_EndSessionLifecycle(t *testing.T) {
	dir := newGitRepo(t)
	// Leave the change uncommitted so capture detects working-tree diff.
	writeFile(t, dir+"/src/a.go", "package src\n")

	if out, code := gitAgent(t, dir, "capture", "--source", "claude-code", "--tool", "Edit"); code != 0 {
		t.Fatalf("capture: exit code %d\noutput: %s", code, out)
	}

	out, code := gitAgent(t, dir, "capture", "--source", "claude-code", "--end-session")
	if code != 0 {
		t.Fatalf("end-session: exit code %d\noutput: %s", code, out)
	}

	// A capture after the session ended must be skipped.
	out, code = gitAgent(t, dir, "capture", "--source", "claude-code", "--tool", "Edit")
	if code != 0 {
		t.Fatalf("post-end capture: exit code %d\noutput: %s", code, out)
	}
	var res graph.CaptureResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("post-end capture output not JSON: %v\n%s", err, out)
	}
	if !res.Skipped {
		t.Errorf("post-end capture: not skipped\noutput: %s", out)
	}
}
