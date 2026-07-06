package graph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"
)

// CurrentSchemaVersion is the schema version written by this git-agent build.
// A database with a higher stored version was produced by a newer binary and
// cannot be read safely.
//
// v4 drops the agent Event Log subsystem (events/event_files/sessions/actions/
// action_modifies/action_produces) — the tables are shed on open via DROP
// statements in schemaStatements, so a v3 database is migrated without a full
// rebuild.
const CurrentSchemaVersion = 4

// GraphDBRelPath is the repo-relative path to the SQLite graph database. It is
// the single source of truth shared by every command that opens the DB and by
// the .gitignore/untrack logic, so the ignore rule and the actual file path
// can never drift apart.
const GraphDBRelPath = ".git-agent/graph.db"

// DBPath returns the absolute graph database path for a repo root.
func DBPath(repoRoot string) string {
	return filepath.Join(repoRoot, GraphDBRelPath)
}

// DirPath returns the repo-relative .git-agent directory that holds the DB.
const DirPath = ".git-agent"

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
		err := c.db.Close()
		c.db = nil
		return err
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
	if err := c.applyMigrations(ctx); err != nil {
		return err
	}
	return c.setSchemaVersion(ctx)
}

// ValidateSchemaVersion rejects databases written by a newer git-agent.
func (c *SQLiteClient) ValidateSchemaVersion(ctx context.Context) error {
	stored, err := c.readSchemaVersion(ctx)
	if err != nil {
		return err
	}
	if stored > CurrentSchemaVersion {
		return fmt.Errorf(
			"graph database schema version %d is newer than this git-agent (supports %d); upgrade git-agent or delete .git-agent/graph.db*",
			stored, CurrentSchemaVersion,
		)
	}
	return nil
}

func (c *SQLiteClient) readSchemaVersion(ctx context.Context) (int, error) {
	// A fresh database has no index_state table yet (validation runs before
	// InitSchema), so treat a missing table as version 0 rather than erroring.
	var tableCount int
	if err := c.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'index_state'`,
	).Scan(&tableCount); err != nil {
		return 0, fmt.Errorf("probe index_state: %w", err)
	}
	if tableCount == 0 {
		return 0, nil
	}

	var val sql.NullString
	err := c.db.QueryRowContext(ctx,
		`SELECT value FROM index_state WHERE key = 'schema_version'`,
	).Scan(&val)
	if err == sql.ErrNoRows || (err == nil && !val.Valid) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read schema_version: %w", err)
	}
	v, err := strconv.Atoi(val.String)
	if err != nil {
		return 0, fmt.Errorf("parse schema_version %q: %w", val.String, err)
	}
	return v, nil
}

func (c *SQLiteClient) setSchemaVersion(ctx context.Context) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO index_state (key, value) VALUES ('schema_version', ?)`,
		strconv.Itoa(CurrentSchemaVersion),
	)
	if err != nil {
		return fmt.Errorf("set schema_version: %w", err)
	}
	return nil
}

// migrations are idempotent column additions for pre-existing databases that
// predate the column. Empty now; kept as the seam for future co-change column
// additions. (Retired tables are shed via DROP statements in schemaStatements,
// not here.)
var migrations = []struct {
	table string
	col   string
	decl  string
}{}

func (c *SQLiteClient) applyMigrations(ctx context.Context) error {
	for _, m := range migrations {
		exists, err := c.columnExists(ctx, m.table, m.col)
		if err != nil {
			return fmt.Errorf("check column %s.%s: %w", m.table, m.col, err)
		}
		if exists {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", m.table, m.col, m.decl)
		if _, err := c.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate add %s.%s: %w", m.table, m.col, err)
		}
	}
	return nil
}

func (c *SQLiteClient) columnExists(ctx context.Context, table, col string) (bool, error) {
	rows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
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

	// Performance indexes
	`CREATE INDEX IF NOT EXISTS idx_commits_timestamp ON commits(timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_modifies_file ON modifies(file_path)`,
	`CREATE INDEX IF NOT EXISTS idx_modifies_commit ON modifies(commit_hash)`,
	`CREATE INDEX IF NOT EXISTS idx_co_changed_file_a ON co_changed(file_a)`,
	`CREATE INDEX IF NOT EXISTS idx_co_changed_file_b ON co_changed(file_b)`,
	`CREATE INDEX IF NOT EXISTS idx_co_changed_strength ON co_changed(coupling_strength)`,
	`CREATE INDEX IF NOT EXISTS idx_renames_old ON renames(old_path)`,
	`CREATE INDEX IF NOT EXISTS idx_renames_new ON renames(new_path)`,

	// Retired layers — dropped on open so a database built before the cut sheds
	// these tables without a full rebuild. Idempotent: a no-op once the tables
	// are gone (and on a fresh DB).
	//
	// Event Log subsystem (schema v3 → v4): the append-only action log and its
	// projections are gone; the graph is now commit-history co-change only.
	`DROP TABLE IF EXISTS event_files`,
	`DROP TABLE IF EXISTS events`,
	`DROP TABLE IF EXISTS action_produces`,
	`DROP TABLE IF EXISTS action_modifies`,
	`DROP TABLE IF EXISTS actions`,
	`DROP TABLE IF EXISTS sessions`,

	// AST layer (schema v2 → v3): the structural call graph is gone.
	`DROP TRIGGER IF EXISTS ast_nodes_fts_ai`,
	`DROP TRIGGER IF EXISTS ast_nodes_fts_ad`,
	`DROP TRIGGER IF EXISTS ast_nodes_fts_au`,
	`DROP TABLE IF EXISTS ast_nodes_fts`,
	`DROP TABLE IF EXISTS ast_nodes`,
	`DROP TABLE IF EXISTS ast_edges`,
	`DROP TABLE IF EXISTS ast_unresolved_refs`,
}
