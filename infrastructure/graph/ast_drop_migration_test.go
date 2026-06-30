package graph

import (
	"context"
	"path/filepath"
	"testing"
)

// TestInitSchema_DropsRetiredASTTables verifies the schema-v3 cleanup: a graph
// database built before the co-change-only refactor (carrying ast_nodes /
// ast_edges / ast_unresolved_refs and the FTS index) sheds those tables on the
// next open via InitSchema, without a full rebuild and without touching the
// co-change tables.
func TestInitSchema_DropsRetiredASTTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	client := NewSQLiteClient(dbPath)
	ctx := context.Background()
	if err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	db := client.DB()

	// Forge a pre-refactor database: the co-change side plus the retired AST
	// tables and a stale row, mirroring schema v2.
	stmts := []string{
		`CREATE TABLE commits (hash TEXT PRIMARY KEY, message TEXT, timestamp INTEGER)`,
		`CREATE TABLE ast_nodes (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE ast_edges (id INTEGER PRIMARY KEY AUTOINCREMENT, source TEXT)`,
		`CREATE TABLE ast_unresolved_refs (id INTEGER PRIMARY KEY AUTOINCREMENT, from_node_id TEXT)`,
		`CREATE VIRTUAL TABLE ast_nodes_fts USING fts5(name, content='ast_nodes', content_rowid='rowid')`,
		`INSERT INTO commits (hash, message, timestamp) VALUES ('c1', 'feat: x', 1)`,
		`INSERT INTO ast_nodes (id, name) VALUES ('n1', 'Foo')`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed old schema %q: %v", s, err)
		}
	}

	// Opening the schema (what openGraphDB does on every command) must drop the
	// AST tables.
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}

	for _, tbl := range []string{"ast_nodes", "ast_edges", "ast_unresolved_refs", "ast_nodes_fts"} {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE name = ?`, tbl,
		).Scan(&n); err != nil {
			t.Fatalf("probe %s: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("retired AST table %q still present after InitSchema", tbl)
		}
	}

	// The co-change side is untouched: the seeded commit survives.
	var commitCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM commits`).Scan(&commitCount); err != nil {
		t.Fatalf("count commits: %v", err)
	}
	if commitCount != 1 {
		t.Errorf("co-change data must be preserved across migration: commits=%d, want 1", commitCount)
	}

	// Schema version is now current.
	got, err := client.readSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("readSchemaVersion: %v", err)
	}
	if got != CurrentSchemaVersion {
		t.Errorf("schema_version = %d, want %d", got, CurrentSchemaVersion)
	}
}
