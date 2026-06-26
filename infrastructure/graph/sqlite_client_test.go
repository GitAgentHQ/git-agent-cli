package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteClient_OpenCreatesDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("Open() did not create database file")
	}
}

func TestSQLiteClient_InitSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}

	// Verify all 14 tables exist.
	wantTables := []string{
		"commits", "files", "authors", "modifies", "authored",
		"co_changed", "renames", "index_state",
		"sessions", "actions", "action_modifies", "action_produces",
		"events", "event_files",
	}

	db := client.DB()
	for _, table := range wantTables {
		var name string
		err := db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestSQLiteClient_InitSchema_Indexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}

	wantIndexes := []string{
		"idx_commits_timestamp",
		"idx_modifies_file",
		"idx_modifies_commit",
		"idx_co_changed_file_a",
		"idx_co_changed_file_b",
		"idx_co_changed_strength",
		"idx_actions_session",
		"idx_actions_timestamp",
		"idx_action_modifies_file",
		"idx_sessions_source_instance",
		"idx_renames_old",
		"idx_renames_new",
	}

	db := client.DB()
	for _, idx := range wantIndexes {
		var name string
		err := db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q not found: %v", idx, err)
		}
	}
}

func TestSQLiteClient_InitSchema_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("first InitSchema() error = %v", err)
	}
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("second InitSchema() error = %v", err)
	}
}

func TestSQLiteClient_ValidateSchemaVersion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}
	if err := client.ValidateSchemaVersion(ctx); err != nil {
		t.Fatalf("ValidateSchemaVersion() error = %v", err)
	}

	if _, err := client.DB().ExecContext(ctx,
		`INSERT OR REPLACE INTO index_state (key, value) VALUES ('schema_version', '999')`,
	); err != nil {
		t.Fatalf("set future schema version: %v", err)
	}
	if err := client.ValidateSchemaVersion(ctx); err == nil {
		t.Fatal("expected error for future schema version")
	}
}

// TestSQLiteClient_ValidateBeforeInitRejectsNewerDB guards the call ordering in
// the CLI entry points: validation must run before InitSchema, because
// InitSchema rewrites schema_version to the current value. It also covers the
// fresh-database path, where validation runs before any table exists.
func TestSQLiteClient_ValidateBeforeInitRejectsNewerDB(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	// Seed a database written by a hypothetical newer binary.
	seed := NewSQLiteClient(dbPath)
	if err := seed.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := seed.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}
	if _, err := seed.DB().ExecContext(ctx,
		`INSERT OR REPLACE INTO index_state (key, value) VALUES ('schema_version', ?)`,
		CurrentSchemaVersion+1,
	); err != nil {
		t.Fatalf("seed future version: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopen and validate BEFORE InitSchema, mirroring the CLI entry points.
	client := NewSQLiteClient(dbPath)
	if err := client.Open(ctx); err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer client.Close()
	if err := client.ValidateSchemaVersion(ctx); err == nil {
		t.Fatal("expected ValidateSchemaVersion to reject a newer database before InitSchema")
	}
}

// TestSQLiteClient_ValidateFreshDatabase verifies validation before InitSchema
// succeeds on a brand-new database where index_state does not yet exist.
func TestSQLiteClient_ValidateFreshDatabase(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	if err := client.ValidateSchemaVersion(ctx); err != nil {
		t.Fatalf("ValidateSchemaVersion() on fresh db error = %v", err)
	}
}

func TestSQLiteClient_Close(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// After Close, the handle is cleared so callers can't reuse a dead connection.
	if client.DB() != nil {
		t.Fatal("expected DB() to be nil after Close()")
	}
}

func TestSQLiteClient_Drop(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	ctx := context.Background()
	if err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}

	if err := client.Drop(ctx); err != nil {
		t.Fatalf("Drop() error = %v", err)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("expected database file to be removed, stat error = %v", err)
	}
}

func TestSQLiteClient_OpenExisting(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	// First connection: create schema and insert data.
	c1 := NewSQLiteClient(dbPath)
	if err := c1.Open(ctx); err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := c1.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}
	_, err := c1.DB().ExecContext(ctx,
		"INSERT INTO index_state (key, value) VALUES (?, ?)", "test_key", "test_value",
	)
	if err != nil {
		t.Fatalf("insert error = %v", err)
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	// Second connection: data should survive.
	c2 := NewSQLiteClient(dbPath)
	if err := c2.Open(ctx); err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer c2.Close()

	var value string
	err = c2.DB().QueryRowContext(ctx,
		"SELECT value FROM index_state WHERE key = ?", "test_key",
	).Scan(&value)
	if err != nil {
		t.Fatalf("query after reopen error = %v", err)
	}
	if value != "test_value" {
		t.Errorf("got value %q, want %q", value, "test_value")
	}
}

func TestSQLiteClient_MigratesActionProducesPrimaryKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	client := NewSQLiteClient(dbPath)
	if err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := client.DB().ExecContext(ctx, `CREATE TABLE action_modifies (
		action_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		additions INTEGER DEFAULT 0,
		deletions INTEGER DEFAULT 0,
		PRIMARY KEY (action_id, file_path)
	)`); err != nil {
		t.Fatalf("create old action_modifies: %v", err)
	}
	if _, err := client.DB().ExecContext(ctx, `CREATE TABLE action_produces (
		action_id TEXT NOT NULL,
		commit_hash TEXT NOT NULL,
		PRIMARY KEY (action_id, commit_hash)
	)`); err != nil {
		t.Fatalf("create old action_produces: %v", err)
	}
	if _, err := client.DB().ExecContext(ctx,
		`INSERT INTO action_modifies (action_id, file_path) VALUES (?, ?), (?, ?)`,
		"action-1", "a.go", "action-1", "b.go",
	); err != nil {
		t.Fatalf("insert action_modifies: %v", err)
	}
	if _, err := client.DB().ExecContext(ctx,
		`INSERT INTO action_produces (action_id, commit_hash) VALUES (?, ?)`,
		"action-1", "commit-1",
	); err != nil {
		t.Fatalf("insert old action_produces: %v", err)
	}

	if err := client.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}
	if _, err := client.DB().ExecContext(ctx,
		`INSERT INTO action_produces (action_id, commit_hash, file_path) VALUES (?, ?, ?)`,
		"action-1", "commit-1", "c.go",
	); err != nil {
		t.Fatalf("insert per-file action_produces after migration: %v", err)
	}

	var count int
	if err := client.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM action_produces WHERE action_id = ? AND commit_hash = ?`,
		"action-1", "commit-1",
	).Scan(&count); err != nil {
		t.Fatalf("count migrated action_produces: %v", err)
	}
	if count != 3 {
		t.Fatalf("migrated action_produces should contain per-file rows, got %d", count)
	}
}

func TestSQLiteClient_WALMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	var mode string
	err := client.DB().QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatalf("PRAGMA journal_mode error = %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestSQLiteClient_BusyTimeout(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	var timeout int
	err := client.DB().QueryRow("PRAGMA busy_timeout").Scan(&timeout)
	if err != nil {
		t.Fatalf("PRAGMA busy_timeout error = %v", err)
	}
	if timeout != 5000 {
		t.Errorf("busy_timeout = %d, want %d", timeout, 5000)
	}
}
