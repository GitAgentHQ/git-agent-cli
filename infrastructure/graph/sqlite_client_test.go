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

	// Verify all 13 tables exist.
	wantTables := []string{
		"commits", "files", "authors", "modifies", "authored",
		"co_changed", "renames", "index_state",
		"sessions", "actions", "action_modifies", "action_produces",
		"capture_baseline",
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

func TestSQLiteClient_Close(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Subsequent query should fail.
	if err := client.DB().Ping(); err == nil {
		t.Fatal("expected error after Close(), got nil")
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

func TestSQLiteClient_ForeignKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client := NewSQLiteClient(dbPath)
	if err := client.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer client.Close()

	var fk int
	err := client.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if err != nil {
		t.Fatalf("PRAGMA foreign_keys error = %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want %d", fk, 1)
	}
}
