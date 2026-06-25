package graph

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	domaingraph "github.com/gitagenthq/git-agent/domain/graph"
)

// forbiddenEventsMutation matches any SQL statement that would mutate the
// append-only events table: UPDATE/DELETE on events, or an INSERT that replaces
// or ignores conflicts (which can overwrite a chained row).
var forbiddenEventsMutation = regexp.MustCompile(
	`(?i)(UPDATE\s+events\b|DELETE\s+FROM\s+events\b|INSERT\s+OR\s+(REPLACE|IGNORE)\s+INTO\s+events\b)`,
)

// TestEventsTableIsAppendOnly_NoProductionMutations scans the production Go
// sources and asserts no code path issues an UPDATE/DELETE/INSERT-OR-REPLACE on
// the events table, then exercises the real append path to confirm it writes a
// row that is never subsequently mutated.
func TestEventsTableIsAppendOnly_NoProductionMutations(t *testing.T) {
	roots := []string{".", filepath.Join("..", "..", "application")}
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if loc := forbiddenEventsMutation.FindIndex(src); loc != nil {
				t.Errorf("%s issues a forbidden mutation on events: %q", path, string(src[loc[0]:loc[1]]))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// Runtime confirmation: the production append path lands exactly one row per
	// append, and that row's this_hash is stable (never rewritten).
	ctx := context.Background()
	repo := newEventTestRepo(t)

	first, err := repo.AppendEvent(ctx, toolEvent("evt-1", 100))
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := repo.AppendEvent(ctx, toolEvent("evt-2", 101)); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	var count int
	if err := repo.Client().DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events`,
	).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 2 {
		t.Fatalf("append-only events count = %d, want 2", count)
	}

	var storedHash string
	if err := repo.Client().DB().QueryRowContext(ctx,
		`SELECT this_hash FROM events WHERE seq = ?`, first.Seq,
	).Scan(&storedHash); err != nil {
		t.Fatalf("read this_hash: %v", err)
	}
	if storedHash != first.ThisHash || storedHash == domaingraph.GenesisHash {
		t.Errorf("stored this_hash = %q, want appended ThisHash %q", storedHash, first.ThisHash)
	}
}
