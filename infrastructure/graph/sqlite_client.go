package graph

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

// SQLiteClient wraps a database/sql connection to a SQLite database
// using the modernc.org/sqlite pure-Go driver.
type SQLiteClient struct {
	dbPath string
	db     *sql.DB
}

// NewSQLiteClient returns a new SQLiteClient for the given database path.
func NewSQLiteClient(dbPath string) *SQLiteClient {
	return &SQLiteClient{dbPath: dbPath}
}

// Open opens (or creates) the SQLite database and configures PRAGMAs.
func (c *SQLiteClient) Open(ctx context.Context) error {
	db, err := sql.Open("sqlite", c.dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}

	// Force a connection so the file is created immediately.
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping sqlite: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return fmt.Errorf("exec %q: %w", p, err)
		}
	}

	c.db = db
	return nil
}

// Close closes the database connection.
func (c *SQLiteClient) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// DB returns the underlying *sql.DB for repository use.
func (c *SQLiteClient) DB() *sql.DB {
	return c.db
}

// InitSchema creates all tables and indexes if they do not already exist.
func (c *SQLiteClient) InitSchema(ctx context.Context) error {
	for _, stmt := range schemaStatements {
		if _, err := c.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec schema: %w\n  statement: %s", err, stmt)
		}
	}
	return nil
}

// Drop closes the database connection and removes the database file.
func (c *SQLiteClient) Drop(ctx context.Context) error {
	if err := c.Close(); err != nil {
		return fmt.Errorf("close before drop: %w", err)
	}
	if err := os.Remove(c.dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove db file: %w", err)
	}
	// Remove WAL and SHM files if present.
	os.Remove(c.dbPath + "-wal")
	os.Remove(c.dbPath + "-shm")
	return nil
}

// schemaStatements contains all CREATE TABLE and CREATE INDEX statements.
var schemaStatements = []string{
	// Co-change layer
	`CREATE TABLE IF NOT EXISTS commits (
		hash TEXT PRIMARY KEY,
		message TEXT,
		author_name TEXT,
		author_email TEXT,
		timestamp INTEGER,
		parent_hashes TEXT
	)`,

	`CREATE TABLE IF NOT EXISTS files (
		path TEXT PRIMARY KEY,
		language TEXT,
		last_indexed_hash TEXT
	)`,

	`CREATE TABLE IF NOT EXISTS authors (
		email TEXT PRIMARY KEY,
		name TEXT
	)`,

	`CREATE TABLE IF NOT EXISTS modifies (
		commit_hash TEXT NOT NULL,
		file_path TEXT NOT NULL,
		additions INTEGER DEFAULT 0,
		deletions INTEGER DEFAULT 0,
		status TEXT,
		PRIMARY KEY (commit_hash, file_path)
	)`,

	`CREATE TABLE IF NOT EXISTS authored (
		author_email TEXT NOT NULL,
		commit_hash TEXT NOT NULL,
		PRIMARY KEY (author_email, commit_hash)
	)`,

	`CREATE TABLE IF NOT EXISTS co_changed (
		file_a TEXT NOT NULL,
		file_b TEXT NOT NULL,
		coupling_count INTEGER DEFAULT 0,
		coupling_strength REAL DEFAULT 0.0,
		last_coupled_hash TEXT,
		PRIMARY KEY (file_a, file_b),
		CHECK (file_a < file_b)
	)`,

	`CREATE TABLE IF NOT EXISTS renames (
		old_path TEXT NOT NULL,
		new_path TEXT NOT NULL,
		commit_hash TEXT NOT NULL,
		PRIMARY KEY (old_path, new_path, commit_hash)
	)`,

	`CREATE TABLE IF NOT EXISTS index_state (
		key TEXT PRIMARY KEY,
		value TEXT
	)`,

	// Action layer
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		instance_id TEXT,
		started_at INTEGER NOT NULL,
		ended_at INTEGER
	)`,

	`CREATE TABLE IF NOT EXISTS actions (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		sequence INTEGER NOT NULL DEFAULT 0,
		tool TEXT,
		diff TEXT,
		files_changed TEXT,
		timestamp INTEGER NOT NULL,
		message TEXT
	)`,

	`CREATE TABLE IF NOT EXISTS action_modifies (
		action_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		additions INTEGER DEFAULT 0,
		deletions INTEGER DEFAULT 0,
		PRIMARY KEY (action_id, file_path)
	)`,

	`CREATE TABLE IF NOT EXISTS action_produces (
		action_id TEXT NOT NULL,
		commit_hash TEXT NOT NULL,
		PRIMARY KEY (action_id, commit_hash)
	)`,

	`CREATE TABLE IF NOT EXISTS capture_baseline (
		file_path TEXT PRIMARY KEY,
		content_hash TEXT NOT NULL,
		captured_at INTEGER NOT NULL
	)`,

	// Performance indexes
	`CREATE INDEX IF NOT EXISTS idx_commits_timestamp ON commits(timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_modifies_file ON modifies(file_path)`,
	`CREATE INDEX IF NOT EXISTS idx_modifies_commit ON modifies(commit_hash)`,
	`CREATE INDEX IF NOT EXISTS idx_co_changed_file_a ON co_changed(file_a)`,
	`CREATE INDEX IF NOT EXISTS idx_co_changed_file_b ON co_changed(file_b)`,
	`CREATE INDEX IF NOT EXISTS idx_co_changed_strength ON co_changed(coupling_strength)`,
	`CREATE INDEX IF NOT EXISTS idx_actions_session ON actions(session_id)`,
	`CREATE INDEX IF NOT EXISTS idx_actions_timestamp ON actions(timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_action_modifies_file ON action_modifies(file_path)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_source_instance ON sessions(source, instance_id)`,
	`CREATE INDEX IF NOT EXISTS idx_renames_old ON renames(old_path)`,
	`CREATE INDEX IF NOT EXISTS idx_renames_new ON renames(new_path)`,
}
