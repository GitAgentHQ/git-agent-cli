package cmd

import (
	"context"
	"path/filepath"
	"testing"

	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

// TestOpenGraphDBConn_DropsRetiredTables verifies commit's existing-graph path
// runs InitSchema migrations (v4 drops retired Event Log tables) without the
// gitignore/bootstrap side effects of openGraphDB.
func TestOpenGraphDBConn_DropsRetiredTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	seed := infraGraph.NewSQLiteClient(dbPath)
	ctx := context.Background()
	if err := seed.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	db := seed.DB()
	stmts := []string{
		`CREATE TABLE commits (hash TEXT PRIMARY KEY, message TEXT, timestamp INTEGER)`,
		`CREATE TABLE events (seq INTEGER PRIMARY KEY AUTOINCREMENT, this_hash TEXT)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY)`,
		`INSERT INTO commits (hash, message, timestamp) VALUES ('c1', 'feat: x', 1)`,
		`INSERT INTO events (seq, this_hash) VALUES (1, 'h1')`,
		`INSERT INTO sessions (id) VALUES ('s1')`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed old schema %q: %v", s, err)
		}
	}
	seed.Close()

	client, err := openGraphDBConn(ctx, dbPath)
	if err != nil {
		t.Fatalf("openGraphDBConn() error = %v", err)
	}
	defer client.Close()

	for _, tbl := range []string{"events", "sessions"} {
		var n int
		if err := client.DB().QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE name = ?`, tbl,
		).Scan(&n); err != nil {
			t.Fatalf("probe %s: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("retired table %q still present after openGraphDBConn", tbl)
		}
	}
}
