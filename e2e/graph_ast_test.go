package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// setupASTFixture builds a small Go call graph with a test file so the AST
// query commands have real edges to traverse:
//
//	handler.go:       runHandler → processData, validateData
//	handler_test.go:  TestValidate → validateData
//
// It returns the repo dir, committed so the AST indexer has a HEAD to index.
func setupASTFixture(t *testing.T) string {
	t.Helper()
	dir := newGitRepo(t)
	writeFile(t, dir+"/go.mod", "module testproject\n\ngo 1.21\n")
	writeFile(t, dir+"/handler.go", `package main

func runHandler(input string) string {
	processed := processData(input)
	validated := validateData(processed)
	return validated
}

func processData(raw string) string {
	return "processed:" + raw
}

func validateData(data string) string {
	return "valid:" + data
}
`)
	writeFile(t, dir+"/handler_test.go", `package main

import "testing"

func TestValidate(t *testing.T) {
	if got := validateData("x"); got != "valid:x" {
		t.Fatalf("got %q", got)
	}
}
`)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func TestGraph_Callers(t *testing.T) {
	dir := setupASTFixture(t)
	out, code := gitAgent(t, dir, "graph", "callers", "validateData", "-o", "text")
	if code != 0 {
		t.Fatalf("callers: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "runHandler") {
		t.Errorf("callers of validateData should include runHandler\ngot: %s", out)
	}
	if !strings.Contains(out, "TestValidate") {
		t.Errorf("callers of validateData should include the test caller TestValidate\ngot: %s", out)
	}
}

func TestGraph_Callees(t *testing.T) {
	dir := setupASTFixture(t)
	out, code := gitAgent(t, dir, "graph", "callees", "runHandler", "-o", "text")
	if code != 0 {
		t.Fatalf("callees: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "processData") || !strings.Contains(out, "validateData") {
		t.Errorf("callees of runHandler should include processData and validateData\ngot: %s", out)
	}
}

func TestGraph_Node(t *testing.T) {
	dir := setupASTFixture(t)
	out, _, code := gitAgentSeparated(t, dir, "graph", "symbol", "validateData", "-o", "json")
	if code != 0 {
		t.Fatalf("node: exit %d\n%s", code, out)
	}
	var res struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("symbol output not JSON: %v\n%s", err, out)
	}
	if len(res.Matches) == 0 {
		t.Fatalf("symbol returned no matches\n%s", out)
	}
	if got, _ := res.Matches[0]["node"].(map[string]any); got["name"] != "validateData" {
		t.Errorf("symbol name = %v, want validateData", got["name"])
	}
}

func TestGraph_Query(t *testing.T) {
	dir := setupASTFixture(t)
	out, _, code := gitAgentSeparated(t, dir, "graph", "search", "validate", "-o", "json")
	if code != 0 {
		t.Fatalf("query: exit %d\n%s", code, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("query output not JSON: %v\n%s", err, out)
	}
	results, _ := res["results"].([]any)
	if len(results) == 0 {
		t.Errorf("query 'validate' should match validateData\n%s", out)
	}
}

// TestGraph_NotIndexedExitsThree locks the exit-3 contract: a graph AST read on
// a repo with no commits (no index can exist yet) exits 3 and, in JSON mode,
// emits a clean error envelope on stderr with an empty stdout.
func TestGraph_NotIndexedExitsThree(t *testing.T) {
	dir := newGitRepo(t) // git init only — no commit, so no committed HEAD to index
	stdout, stderr, code := gitAgentSeparated(t, dir, "graph", "search", "Foo", "-o", "json")
	if code != 3 {
		t.Fatalf("graph search on a no-commit repo: exit %d (want 3)\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout should be empty on error, got: %s", stdout)
	}
	var env struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr is not a JSON error envelope: %v\n%s", err, stderr)
	}
	if env.Error.Code != 3 {
		t.Errorf("error.code = %d, want 3", env.Error.Code)
	}
}

func TestGraph_Affected(t *testing.T) {
	dir := setupASTFixture(t)
	// validateData lives in handler.go; the test file handler_test.go calls it.
	out, code := gitAgent(t, dir, "graph", "affected", "handler.go", "-o", "text")
	if code != 0 {
		t.Fatalf("affected: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "handler_test.go") {
		t.Errorf("affected(handler.go) should name handler_test.go as a test to run\ngot: %s", out)
	}
}

func TestGraph_Sync_NoOpWhenCurrent(t *testing.T) {
	dir := newGitRepo(t)

	payload := []byte(`{"session_id":"sess-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Edit","tool_input":{"file_path":"src/a.go","old_string":"x","new_string":"y"}}`)
	if out, code := gitAgentStdin(t, dir, payload, "capture", "--source", "claude-code"); code != 0 {
		t.Fatalf("capture: exit %d\n%s", code, out)
	}
	if out, code := gitAgent(t, dir, "graph", "index"); code != 0 {
		t.Fatalf("index: exit %d\n%s", code, out)
	}

	// Projections already reflect the latest event: sync is a no-op.
	out, code := gitAgent(t, dir, "graph", "sync", "-o", "json")
	if code != 0 {
		t.Fatalf("sync: exit %d\n%s", code, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("sync output not JSON: %v\n%s", err, out)
	}
	if res["up_to_date"] != true {
		t.Errorf("sync up_to_date = %v, want true\n%s", res["up_to_date"], out)
	}
}

func TestGraph_Sync_ReplaysWhenStale(t *testing.T) {
	dir := newGitRepo(t)

	payload := []byte(`{"session_id":"sess-1","hook_event_name":"PostToolUse",` +
		`"tool_name":"Edit","tool_input":{"file_path":"src/a.go","old_string":"x","new_string":"y"}}`)
	if out, code := gitAgentStdin(t, dir, payload, "capture", "--source", "claude-code"); code != 0 {
		t.Fatalf("capture: exit %d\n%s", code, out)
	}
	// No index yet: projections lag the Event Log, so sync must replay.
	out, code := gitAgent(t, dir, "graph", "sync", "-o", "json")
	if code != 0 {
		t.Fatalf("sync: exit %d\n%s", code, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("sync output not JSON: %v\n%s", err, out)
	}
	if res["up_to_date"] == true {
		t.Errorf("sync should not be up_to_date when projections lag\n%s", out)
	}
	// After sync, timeline reflects the captured action.
	out, code = gitAgent(t, dir, "audit", "timeline", "-o", "json")
	if code != 0 {
		t.Fatalf("timeline: exit %d\n%s", code, out)
	}
	var tl graph.TimelineResult
	if err := json.Unmarshal([]byte(out), &tl); err != nil {
		t.Fatalf("timeline not JSON: %v\n%s", err, out)
	}
	if tl.TotalActions != 1 {
		t.Errorf("TotalActions = %d, want 1 after sync\n%s", tl.TotalActions, out)
	}
}
