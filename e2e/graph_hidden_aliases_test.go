package e2e_test

import (
	"strings"
	"testing"
)

// TestGraph_IndexAndSync_HiddenButUsable locks the retirement contract: after
// the git-first generation model landed, `graph index` and `graph sync` are
// hidden from `graph --help` (building is automatic via commit + read-path
// auto-sync) but the names still work as compatibility aliases for scripts.
func TestGraph_IndexAndSync_HiddenButUsable(t *testing.T) {
	dir := newGitRepo(t)

	help, code := gitAgent(t, dir, "graph", "--help")
	if code != 0 {
		t.Fatalf("graph --help: exit %d\n%s", code, help)
	}
	// Hidden commands must not appear in the listing.
	for _, name := range []string{"index", "sync"} {
		// A bare word match is too loose (e.g. "index" appears in other Short
		// blurbs), so match the command's own help line: "  index   ..." /
		// "  sync    ..." at line start.
		for _, line := range strings.Split(help, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, name+" ") || trimmed == name {
				t.Errorf("%q must be hidden from `graph --help`, but found line: %q", name, line)
			}
		}
	}

	// But the names must still run (compatibility alias).
	for _, name := range []string{"index", "sync"} {
		out, code := gitAgent(t, dir, "graph", name)
		if code != 0 {
			t.Errorf("`graph %s` (alias) must still work, got exit %d\n%s", name, code, out)
		}
	}
}
