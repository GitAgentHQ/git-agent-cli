package e2e_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// TestRelated_ReportsCoChangeWithLinkingCommits exercises the primary agent
// interface end-to-end: `related <file>` mines git history for the files that
// change together with the seed and, in JSON, attaches the commits that prove
// the coupling (subject + sha + ts). It also checks --tests narrows the result
// to test files. Co-change is language-agnostic, so the fixture uses plain text.
func TestRelated_ReportsCoChangeWithLinkingCommits(t *testing.T) {
	dir := newGitRepo(t)

	token := filepath.Join(dir, "auth", "token.txt")
	mw := filepath.Join(dir, "auth", "middleware.txt")
	tokenTest := filepath.Join(dir, "auth", "token_test.txt")

	// Three commits that change token, middleware and the test together, with
	// auth-themed subjects — enough to clear the default --min-count of 3.
	subjects := []string{
		"feat(auth): add token store",
		"fix(auth): guard nil token",
		"refactor(auth): token refresh",
	}
	for i, subj := range subjects {
		writeFile(t, token, "token v"+string(rune('0'+i))+"\n")
		writeFile(t, mw, "mw v"+string(rune('0'+i))+"\n")
		writeFile(t, tokenTest, "test v"+string(rune('0'+i))+"\n")
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-q", "-m", subj)
	}

	// Build the co-change index (no LLM needed).
	if out, code := gitAgent(t, dir, "init", "--graph"); code != 0 {
		t.Fatalf("init --graph: exit %d\n%s", code, out)
	}

	// related auth/token.txt -> JSON with middleware + the linking commits.
	out, code := gitAgent(t, dir, "related", "auth/token.txt", "-o", "json")
	if code != 0 {
		t.Fatalf("related: exit %d\n%s", code, out)
	}
	var result graph.ImpactResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("related output not JSON: %v\n%s", err, out)
	}

	var mwEntry *graph.ImpactEntry
	for i := range result.CoChanged {
		if result.CoChanged[i].Path == "auth/middleware.txt" {
			mwEntry = &result.CoChanged[i]
		}
	}
	if mwEntry == nil {
		t.Fatalf("expected auth/middleware.txt in related set, got: %s", out)
	}
	if len(mwEntry.LinkingCommits) == 0 {
		t.Fatalf("expected linking commits on the related entry, got: %s", out)
	}
	// The most-recent linking commit's subject must be carried through.
	if mwEntry.LinkingCommits[0].Subject != "refactor(auth): token refresh" {
		t.Errorf("unexpected top linking subject %q\n%s", mwEntry.LinkingCommits[0].Subject, out)
	}

	// --tests narrows to the related test file only.
	testsOut, code := gitAgent(t, dir, "related", "auth/token.txt", "--tests", "-o", "json")
	if code != 0 {
		t.Fatalf("related --tests: exit %d\n%s", code, testsOut)
	}
	var testsResult graph.ImpactResult
	if err := json.Unmarshal([]byte(testsOut), &testsResult); err != nil {
		t.Fatalf("related --tests output not JSON: %v\n%s", err, testsOut)
	}
	if len(testsResult.CoChanged) == 0 {
		t.Fatalf("--tests should still return the related test file, got: %s", testsOut)
	}
	for _, e := range testsResult.CoChanged {
		if !strings.Contains(e.Path, "test") {
			t.Errorf("--tests returned a non-test file: %q", e.Path)
		}
	}
}
