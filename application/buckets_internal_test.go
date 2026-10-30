package application

import (
	"testing"

	"github.com/gitagenthq/git-agent/domain/diff"
)

func TestDiffEvidence_SummarizesEachFileWithoutTheBody(t *testing.T) {
	d := &diff.StagedDiff{
		Files: []string{"application/commit_service.go", "cmd/commit.go"},
		Content: "diff --git a/application/commit_service.go b/application/commit_service.go\n" +
			"--- a/application/commit_service.go\n" +
			"+++ b/application/commit_service.go\n" +
			"@@ -10,3 +10,4 @@ func (s *CommitService) commitGroups\n" +
			"+\tadded()\n" +
			"-\tremoved()\n" +
			" context line\n" +
			"diff --git a/cmd/commit.go b/cmd/commit.go\n" +
			"--- a/cmd/commit.go\n" +
			"+++ b/cmd/commit.go\n" +
			"@@ -3,2 +3,3 @@ func runCommit\n" +
			"+\tflag()\n",
	}

	got := diffEvidence(d)

	if len(got) != 2 {
		t.Fatalf("expected one entry per changed file, got %d: %v", len(got), got)
	}
	app := got["application/commit_service.go"]
	if app == nil {
		t.Fatalf("expected an entry for the application file, got %v", got)
	}
	if app.adds != 1 || app.dels != 1 {
		t.Errorf("expected 1 added and 1 removed line, got +%d/-%d", app.adds, app.dels)
	}
	if len(app.symbols) != 1 || app.symbols[0] != "func (s *CommitService) commitGroups" {
		t.Errorf("expected the hunk symbol, got %v", app.symbols)
	}
	cmd := got["cmd/commit.go"]
	if cmd == nil || cmd.adds != 1 {
		t.Errorf("expected 1 added line for cmd/commit.go, got %+v", cmd)
	}
}

func TestBuildBuckets_GroupsByTopLevelDirectoryWithEvidence(t *testing.T) {
	d := &diff.StagedDiff{
		Files: []string{"application/a.go", "application/b.go", "cmd/c.go"},
		Content: "--- a/application/a.go\n" +
			"+++ b/application/a.go\n" +
			"@@ -1 +1,2 @@ func Alpha\n" +
			"+\tone()\n" +
			"--- a/application/b.go\n" +
			"+++ b/application/b.go\n" +
			"@@ -1 +1,2 @@ func Beta\n" +
			"+\ttwo()\n" +
			"--- a/cmd/c.go\n" +
			"+++ b/cmd/c.go\n" +
			"@@ -1 +1,2 @@ func Gamma\n" +
			"+\tthree()\n",
	}

	buckets := buildBuckets([]*diff.StagedDiff{d}, "", 5)

	if len(buckets) != 2 {
		t.Fatalf("expected two buckets, got %+v", buckets)
	}
	app := buckets[0]
	if app.Label != "application" || len(app.Files) != 2 {
		t.Errorf("expected application to hold two files, got %+v", app)
	}
	if app.Adds != 2 {
		t.Errorf("expected two added lines in the application bucket, got %d", app.Adds)
	}
	if len(app.Symbols) != 2 {
		t.Errorf("expected both symbols, got %v", app.Symbols)
	}
	if buckets[1].ID != "b" || buckets[1].Label != "cmd" {
		t.Errorf("expected the second bucket to be cmd, got %+v", buckets[1])
	}
}

func TestBuildBuckets_CapsEveryFileIntoTheLastBucket(t *testing.T) {
	var files []string
	var content string
	for _, dir := range []string{"a", "b", "c", "d"} {
		files = append(files, dir+"/x.go")
		content += "--- a/" + dir + "/x.go\n+++ b/" + dir + "/x.go\n@@ -1 +1,2 @@ func F\n+\tline()\n"
	}

	buckets := buildBuckets([]*diff.StagedDiff{{Files: files, Content: content}}, "", 2)

	total := 0
	for _, b := range buckets {
		total += len(b.Files)
	}
	if total != len(files) {
		t.Errorf("expected every file to survive the cap, got %d of %d", total, len(files))
	}
}

func TestBuildBuckets_ReadsLineCountsFromNumstat(t *testing.T) {
	// Given only file lists, which is what the planning step has.
	d := &diff.StagedDiff{Files: []string{"application/a.go", "cmd/c.go"}}
	numstat := "12\t3\tapplication/a.go\n40\t7\tcmd/c.go\n"

	buckets := buildBuckets([]*diff.StagedDiff{d}, numstat, 5)

	if len(buckets) != 2 {
		t.Fatalf("expected two buckets, got %+v", buckets)
	}
	if buckets[0].Adds != 12 || buckets[0].Dels != 3 {
		t.Errorf("expected 12 added and 3 removed lines, got +%d/-%d", buckets[0].Adds, buckets[0].Dels)
	}
	if buckets[1].Adds != 40 || buckets[1].Dels != 7 {
		t.Errorf("expected 40 added and 7 removed lines, got +%d/-%d", buckets[1].Adds, buckets[1].Dels)
	}
}

func TestParseNumstat_SkipsBinaryAndMalformedRows(t *testing.T) {
	got := parseNumstat("-\t-\tassets/logo.png\nnot a row\n5\t1\tmain.go\n")

	if len(got) != 1 {
		t.Fatalf("expected only the countable row, got %v", got)
	}
	if got["main.go"].adds != 5 || got["main.go"].dels != 1 {
		t.Errorf("expected 5 added and 1 removed line, got %+v", got["main.go"])
	}
}

func TestBuildBuckets_NoDiffIsNoBuckets(t *testing.T) {
	if got := buildBuckets(nil, "", 5); got != nil {
		t.Errorf("expected no buckets without files, got %+v", got)
	}
	if got := buildBuckets([]*diff.StagedDiff{{}}, "", 5); got != nil {
		t.Errorf("expected no buckets for an empty diff, got %+v", got)
	}
}

func TestHunkSymbol_IgnoresAMalformedHeader(t *testing.T) {
	if got := hunkSymbol("@@ -1 +1,2 @@"); got != "" {
		t.Errorf("expected no symbol from a header without a trailing context, got %q", got)
	}
	if got := hunkSymbol("no header here"); got != "" {
		t.Errorf("expected no symbol from a line without a header, got %q", got)
	}
}
