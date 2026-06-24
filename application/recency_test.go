package application

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/domain/graph"
	gitinfra "github.com/gitagenthq/git-agent/infrastructure/git"
)

// TestRecencyWeighting verifies that, with equal co-change counts, a recent
// coupling outranks a stale one — the property the backtest showed lifts recall
// on mature repositories.
func TestRecencyWeighting(t *testing.T) {
	dir := t.TempDir()
	commit := func(date, body string, files ...string) {
		t.Helper()
		for _, f := range files {
			p := filepath.Join(dir, f)
			cur, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(cur, []byte(body+"\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		env := append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t.com", "HOME="+dir,
			"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", body}} {
			c := exec.Command("git", args...)
			c.Dir, c.Env = dir, env
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	for _, a := range [][]string{{"init"}, {"config", "user.name", "T"}, {"config", "user.email", "t@t.com"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.Env = append(os.Environ(), "HOME="+dir)
		c.Run()
	}

	// A.go co-changes with B.go in the distant past, and with C.go recently —
	// identical counts (3 each). Recency must rank C above B.
	for i := 0; i < 3; i++ {
		commit(fmt.Sprintf("2019-01-%02dT00:00:00", i+1), fmt.Sprintf("old%d", i), "A.go", "B.go")
	}
	for i := 0; i < 3; i++ {
		commit(fmt.Sprintf("2026-06-%02dT00:00:00", i+1), fmt.Sprintf("new%d", i), "A.go", "C.go")
	}

	repo := openTestDB(t, dir)
	ctx := context.Background()
	if _, err := NewIndexService(repo, gitinfra.NewGraphClient(dir)).FullIndex(ctx, graph.IndexRequest{}); err != nil {
		t.Fatalf("FullIndex: %v", err)
	}

	res, err := NewImpactService(repo).Impact(ctx, graph.ImpactRequest{Paths: []string{"A.go"}, MinCount: 2})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	strength := map[string]float64{}
	for _, e := range res.CoChanged {
		strength[e.Path] = e.CouplingStrength
	}
	if strength["C.go"] == 0 || strength["B.go"] == 0 {
		t.Fatalf("expected both B.go and C.go in results, got %+v", res.CoChanged)
	}
	if strength["C.go"] <= strength["B.go"] {
		t.Errorf("recent C.go strength (%.3f) must exceed stale B.go strength (%.3f)",
			strength["C.go"], strength["B.go"])
	}
	if res.CoChanged[0].Path != "C.go" {
		t.Errorf("recent coupling should rank first, got %q", res.CoChanged[0].Path)
	}
}
