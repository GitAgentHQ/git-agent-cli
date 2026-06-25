package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// isBusyErr reports whether err is a SQLITE_BUSY result from the driver. The
// primary result code lives in the low 8 bits, so extended busy codes
// (BUSY_SNAPSHOT, BUSY_TIMEOUT) match too.
func isBusyErr(err error) bool {
	var se *sqlitedriver.Error
	if !errors.As(err, &se) {
		return false
	}
	return se.Code()&0xff == sqlite3.SQLITE_BUSY
}

// Compile-time check that SQLiteRepository satisfies GraphRepository.
var _ graph.GraphRepository = (*SQLiteRepository)(nil)

// coChangeHalfLifeDays is the recency half-life for co-change weighting: a
// co-change this many days old contributes half the strength of a fresh one.
const coChangeHalfLifeDays = 365

// SQLiteRepository implements graph.GraphRepository using a SQLiteClient.
type SQLiteRepository struct {
	client    *SQLiteClient
	tx        *sql.Tx              // non-nil while a RunInTx batch is open
	stmtCache map[string]*sql.Stmt // prepared statements reused within the batch
	hasher    graph.EventHasher    // computes the Event chain this_hash
}

// NewSQLiteRepository returns a new SQLiteRepository wrapping the given client.
func NewSQLiteRepository(client *SQLiteClient) *SQLiteRepository {
	return &SQLiteRepository{client: client, hasher: NewSHA256Hasher()}
}

// Client returns the underlying SQLiteClient (used by tests to run raw queries).
func (r *SQLiteRepository) Client() *SQLiteClient {
	return r.client
}

func (r *SQLiteRepository) db() *sql.DB {
	return r.client.DB()
}

// RunInTx executes fn inside one transaction. Every upsert fn issues through
// execStmt is staged and committed together — on a large history this turns tens
// of thousands of autocommits into a single commit (the dominant index cost).
// Methods that manage their own transaction (the co-change recompute) must be
// called outside fn.
func (r *SQLiteRepository) RunInTx(ctx context.Context, fn func() error) error {
	if r.tx != nil {
		return fn() // already batching — reuse the open transaction
	}
	tx, err := r.client.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin batch tx: %w", err)
	}
	r.tx = tx
	r.stmtCache = make(map[string]*sql.Stmt)
	defer func() {
		for _, st := range r.stmtCache {
			st.Close()
		}
		r.stmtCache = nil
		r.tx = nil
	}()
	if err := fn(); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// execStmt runs a write through a prepared statement reused for the lifetime of
// the batch transaction, avoiding re-compilation of the same INSERT on every
// row (the dominant cost under the pure-Go SQLite driver). Outside a batch it
// falls back to a plain autocommit exec.
func (r *SQLiteRepository) execStmt(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if r.tx == nil {
		return r.client.DB().ExecContext(ctx, query, args...)
	}
	st := r.stmtCache[query]
	if st == nil {
		prepared, err := r.tx.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		r.stmtCache[query] = prepared
		st = prepared
	}
	return st.ExecContext(ctx, args...)
}

// --- Lifecycle ---

func (r *SQLiteRepository) Open(ctx context.Context) error {
	return r.client.Open(ctx)
}

func (r *SQLiteRepository) Close() error {
	return r.client.Close()
}

func (r *SQLiteRepository) InitSchema(ctx context.Context) error {
	return r.client.InitSchema(ctx)
}

func (r *SQLiteRepository) Drop(ctx context.Context) error {
	return r.client.Drop(ctx)
}

func (r *SQLiteRepository) ResetIndexData(ctx context.Context) error {
	tx, err := r.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reset index tx: %w", err)
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`DELETE FROM co_changed`,
		`DELETE FROM renames`,
		`DELETE FROM modifies`,
		`DELETE FROM authored`,
		`DELETE FROM commits`,
		`DELETE FROM files`,
		`DELETE FROM authors`,
		`DELETE FROM index_state WHERE key = 'last_indexed_commit'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("reset index data: %w", err)
		}
	}
	return tx.Commit()
}

// --- Indexing writes ---

func (r *SQLiteRepository) UpsertCommit(ctx context.Context, c graph.CommitNode) error {
	parentsJSON, err := json.Marshal(c.ParentHashes)
	if err != nil {
		return fmt.Errorf("marshal parent_hashes: %w", err)
	}
	_, err = r.execStmt(ctx,
		`INSERT OR IGNORE INTO commits (hash, message, author_name, author_email, timestamp, parent_hashes)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.Hash, c.Message, c.AuthorName, c.AuthorEmail, c.Timestamp, string(parentsJSON),
	)
	return err
}

func (r *SQLiteRepository) UpsertAuthor(ctx context.Context, a graph.AuthorNode) error {
	_, err := r.execStmt(ctx,
		`INSERT OR REPLACE INTO authors (email, name) VALUES (?, ?)`,
		a.Email, a.Name,
	)
	return err
}

func (r *SQLiteRepository) UpsertFile(ctx context.Context, f graph.FileNode) error {
	_, err := r.execStmt(ctx,
		`INSERT OR IGNORE INTO files (path) VALUES (?)`,
		f.Path,
	)
	return err
}

func (r *SQLiteRepository) CreateModifies(ctx context.Context, e graph.ModifiesEdge) error {
	_, err := r.execStmt(ctx,
		`INSERT OR IGNORE INTO modifies (commit_hash, file_path, additions, deletions, status)
		 VALUES (?, ?, ?, ?, ?)`,
		e.CommitHash, e.FilePath, e.Additions, e.Deletions, e.Status,
	)
	return err
}

func (r *SQLiteRepository) CreateAuthored(ctx context.Context, authorEmail, commitHash string) error {
	_, err := r.execStmt(ctx,
		`INSERT OR IGNORE INTO authored (author_email, commit_hash) VALUES (?, ?)`,
		authorEmail, commitHash,
	)
	return err
}

func (r *SQLiteRepository) CreateRename(ctx context.Context, oldPath, newPath, commitHash string) error {
	_, err := r.execStmt(ctx,
		`INSERT OR IGNORE INTO renames (old_path, new_path, commit_hash) VALUES (?, ?, ?)`,
		oldPath, newPath, commitHash,
	)
	return err
}

// --- Index state ---

func (r *SQLiteRepository) GetLastIndexedCommit(ctx context.Context) (string, error) {
	return r.GetIndexState(ctx, "last_indexed_commit")
}

func (r *SQLiteRepository) SetLastIndexedCommit(ctx context.Context, hash string) error {
	return r.SetIndexState(ctx, "last_indexed_commit", hash)
}

func (r *SQLiteRepository) GetIndexState(ctx context.Context, key string) (string, error) {
	var val sql.NullString
	err := r.db().QueryRowContext(ctx,
		`SELECT value FROM index_state WHERE key = ?`, key,
	).Scan(&val)
	if err == sql.ErrNoRows || !val.Valid {
		return "", nil
	}
	return val.String, err
}

func (r *SQLiteRepository) SetIndexState(ctx context.Context, key, value string) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR REPLACE INTO index_state (key, value) VALUES (?, ?)`,
		key, value,
	)
	return err
}

func (r *SQLiteRepository) GetSchemaVersion(ctx context.Context) (int, error) {
	var val sql.NullString
	err := r.db().QueryRowContext(ctx,
		`SELECT value FROM index_state WHERE key = ?`, "schema_version",
	).Scan(&val)
	if err == sql.ErrNoRows || !val.Valid {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(val.String)
	if err != nil {
		return 0, fmt.Errorf("parse schema_version %q: %w", val.String, err)
	}
	return v, nil
}

func (r *SQLiteRepository) SetSchemaVersion(ctx context.Context, version int) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR REPLACE INTO index_state (key, value) VALUES (?, ?)`,
		"schema_version", strconv.Itoa(version),
	)
	return err
}

// --- Stats ---

func (r *SQLiteRepository) GetStats(ctx context.Context) (*graph.GraphStats, error) {
	stats := &graph.GraphStats{Exists: true}

	lastCommit, err := r.GetLastIndexedCommit(ctx)
	if err != nil {
		return nil, fmt.Errorf("get last indexed commit: %w", err)
	}
	stats.LastIndexedCommit = lastCommit

	// Table names are hardcoded constants below — safe for fmt.Sprintf.
	tables := []struct {
		name string
		dest *int
	}{
		{"commits", &stats.CommitCount},
		{"files", &stats.FileCount},
		{"authors", &stats.AuthorCount},
		{"co_changed", &stats.CoChangedCount},
		{"sessions", &stats.SessionCount},
		{"actions", &stats.ActionCount},
	}

	for _, t := range tables {
		err := r.db().QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", t.name),
		).Scan(t.dest)
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", t.name, err)
		}
	}

	return stats, nil
}

// recencyCoChangeQuery builds the INSERT that (re)computes co_changed with
// recency-weighted coupling strength: each co-change is weighted by an
// exponential decay of its commit age (half-life coChangeHalfLifeDays), so
// recent couplings dominate while stale ones fade. coupling_count stays the raw
// occurrence count, which the min-count floor still uses. pairFilter, when
// non-empty, is a WHERE clause over w1.file_path/w2.file_path that restricts the
// recompute to pairs touching specific files (the incremental path).
//
// Backtested win: on mature repos (express, flask) this lifts top-10 recall
// ~6-9 points over all-time symmetric strength; on young repos it is neutral.
func recencyCoChangeQuery(pairFilter string) string {
	return fmt.Sprintf(`
INSERT INTO co_changed (file_a, file_b, coupling_count, coupling_strength, last_coupled_hash)
WITH ref AS (SELECT MAX(timestamp) AS t FROM commits),
weighted AS (
    SELECT m.file_path, m.commit_hash,
           exp(-0.6931471805599453 * (ref.t - c.timestamp) / 86400.0 / %d.0) AS w
    FROM modifies m JOIN commits c ON c.hash = m.commit_hash CROSS JOIN ref
),
file_weight AS (
    SELECT file_path, SUM(w) AS total FROM weighted GROUP BY file_path
),
valid_commits AS (
    SELECT commit_hash FROM modifies GROUP BY commit_hash HAVING COUNT(*) <= ?
)
SELECT
    w1.file_path AS file_a,
    w2.file_path AS file_b,
    COUNT(DISTINCT w1.commit_hash) AS coupling_count,
    SUM(w1.w) / MAX(fw1.total, fw2.total) AS coupling_strength,
    MAX(w1.commit_hash) AS last_coupled_hash
FROM weighted w1
JOIN weighted w2 ON w1.commit_hash = w2.commit_hash AND w1.file_path < w2.file_path
JOIN valid_commits vc ON vc.commit_hash = w1.commit_hash
JOIN file_weight fw1 ON fw1.file_path = w1.file_path
JOIN file_weight fw2 ON fw2.file_path = w2.file_path
%s
GROUP BY w1.file_path, w2.file_path
HAVING COUNT(DISTINCT w1.commit_hash) >= ?`, coChangeHalfLifeDays, pairFilter)
}

func (r *SQLiteRepository) RecomputeCoChanged(ctx context.Context, minCount, maxFilesPerCommit int) error {
	tx, err := r.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM co_changed"); err != nil {
		return fmt.Errorf("delete co_changed: %w", err)
	}

	// Give the planner table statistics; without them the pure-Go SQLite engine
	// picks a nested-loop plan for the self-join below and runs orders of
	// magnitude slower on a large history.
	if _, err := tx.ExecContext(ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	if _, err := tx.ExecContext(ctx, recencyCoChangeQuery(""), maxFilesPerCommit, minCount); err != nil {
		return fmt.Errorf("insert co_changed: %w", err)
	}

	return tx.Commit()
}

func (r *SQLiteRepository) IncrementalCoChanged(ctx context.Context, touchedFiles []string, minCount, maxFilesPerCommit int) error {
	if len(touchedFiles) == 0 {
		return nil
	}

	tx, err := r.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Build placeholders for the touched files list.
	placeholders := strings.Repeat("?,", len(touchedFiles)-1) + "?"
	deleteArgs := make([]any, 0, len(touchedFiles)*2)
	for _, f := range touchedFiles {
		deleteArgs = append(deleteArgs, f)
	}
	for _, f := range touchedFiles {
		deleteArgs = append(deleteArgs, f)
	}

	deleteQuery := fmt.Sprintf(
		"DELETE FROM co_changed WHERE file_a IN (%s) OR file_b IN (%s)",
		placeholders, placeholders,
	)
	if _, err := tx.ExecContext(ctx, deleteQuery, deleteArgs...); err != nil {
		return fmt.Errorf("delete touched co_changed: %w", err)
	}

	// Build args for the insert query: maxFilesPerCommit, touchedFiles (x2), minCount
	insertArgs := make([]any, 0, 2+len(touchedFiles)*2)
	insertArgs = append(insertArgs, maxFilesPerCommit)
	for _, f := range touchedFiles {
		insertArgs = append(insertArgs, f)
	}
	for _, f := range touchedFiles {
		insertArgs = append(insertArgs, f)
	}
	insertArgs = append(insertArgs, minCount)

	pairFilter := fmt.Sprintf("WHERE w1.file_path IN (%s) OR w2.file_path IN (%s)", placeholders, placeholders)
	if _, err := tx.ExecContext(ctx, recencyCoChangeQuery(pairFilter), insertArgs...); err != nil {
		return fmt.Errorf("insert incremental co_changed: %w", err)
	}

	return tx.Commit()
}

// coNeighbor is one row from the co_changed table.
type coNeighbor struct {
	path     string
	count    int
	strength float64
}

// coChangedNeighbors returns the files co-changed with path at or above minCount.
func (r *SQLiteRepository) coChangedNeighbors(ctx context.Context, path string, minCount int) ([]coNeighbor, error) {
	rows, err := r.db().QueryContext(ctx,
		`SELECT
			CASE WHEN cc.file_a = ? THEN cc.file_b ELSE cc.file_a END AS neighbor,
			cc.coupling_count,
			cc.coupling_strength
		FROM co_changed cc
		WHERE (cc.file_a = ? OR cc.file_b = ?)
		  AND cc.coupling_count >= ?
		ORDER BY cc.coupling_strength DESC`,
		path, path, path, minCount,
	)
	if err != nil {
		return nil, fmt.Errorf("query co_changed for %q: %w", path, err)
	}
	defer rows.Close()

	var out []coNeighbor
	for rows.Next() {
		var n coNeighbor
		if err := rows.Scan(&n.path, &n.count, &n.strength); err != nil {
			return nil, fmt.Errorf("scan co_changed: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Impact aggregates the co-change neighbours of one or more seed paths. A file
// coupled to several seeds accumulates score (sum of strengths) and seed_matches,
// so the files most central to the changed feature rank first. Transitive
// neighbours (depth > 1) are appended via BFS with their single-edge strength.
//
// Ranking uses the stored symmetric coupling strength (count / max(totalA,
// totalB)) rather than directional P(neighbour | seed): a backtest showed
// directional gives no recall gain while promoting high-fanout noise such as
// changelogs to the top. The symmetric denominator suppresses that noise.
func (r *SQLiteRepository) Impact(ctx context.Context, req graph.ImpactRequest) (*graph.ImpactResult, error) {
	start := time.Now()

	depth := req.Depth
	if depth < 1 {
		depth = 1
	}
	top := req.Top
	if top <= 0 {
		top = 20
	}
	minCount := req.MinCount
	if minCount <= 0 {
		minCount = 3
	}

	// Resolve rename aliases per seed; seeds (and aliases) never appear as results.
	type seedQuery struct {
		display string
		paths   []string
	}
	var seedQueries []seedQuery
	visited := make(map[string]bool)
	for _, s := range req.Paths {
		aliases, err := r.ResolveRenames(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("resolve renames: %w", err)
		}
		paths := append([]string{s}, aliases...)
		seedQueries = append(seedQueries, seedQuery{display: s, paths: paths})
		for _, p := range paths {
			visited[p] = true
		}
	}

	// Depth 1: aggregate neighbours across all seeds.
	agg := make(map[string]*graph.ImpactEntry)
	for _, sq := range seedQueries {
		// Collapse this seed's aliases into one strength/count per neighbour.
		perSeed := make(map[string]coNeighbor)
		for _, p := range sq.paths {
			neighbors, err := r.coChangedNeighbors(ctx, p, minCount)
			if err != nil {
				return nil, err
			}
			for _, nb := range neighbors {
				if visited[nb.path] {
					continue
				}
				cur := perSeed[nb.path]
				cur.count += nb.count
				if nb.strength > cur.strength {
					cur.strength = nb.strength
				}
				perSeed[nb.path] = cur
			}
		}
		for nbPath, v := range perSeed {
			e, ok := agg[nbPath]
			if !ok {
				e = &graph.ImpactEntry{Path: nbPath, Depth: 1}
				agg[nbPath] = e
			}
			e.Score += v.strength
			e.CouplingCount += v.count
			if v.strength > e.CouplingStrength {
				e.CouplingStrength = v.strength
			}
			e.SeedMatches++
			e.RelatedTo = append(e.RelatedTo, sq.display)
		}
	}

	var allEntries []graph.ImpactEntry
	var frontier []string
	for path, e := range agg {
		sort.Strings(e.RelatedTo)
		allEntries = append(allEntries, *e)
		visited[path] = true
		frontier = append(frontier, path)
	}

	// Depth 2..N: transitive BFS, each new file keyed by its single best edge.
	for d := 2; d <= depth; d++ {
		if len(frontier) == 0 {
			break
		}
		best := make(map[string]*graph.ImpactEntry)
		for _, p := range frontier {
			neighbors, err := r.coChangedNeighbors(ctx, p, minCount)
			if err != nil {
				return nil, err
			}
			for _, nb := range neighbors {
				if visited[nb.path] {
					continue
				}
				if existing, ok := best[nb.path]; !ok || nb.strength > existing.CouplingStrength {
					best[nb.path] = &graph.ImpactEntry{
						Path:             nb.path,
						CouplingCount:    nb.count,
						CouplingStrength: nb.strength,
						Score:            nb.strength,
						Depth:            d,
					}
				}
			}
		}
		var next []string
		for path, e := range best {
			allEntries = append(allEntries, *e)
			visited[path] = true
			next = append(next, path)
		}
		frontier = next
	}

	totalFound := len(allEntries)
	sortImpactEntries(allEntries)
	if len(allEntries) > top {
		allEntries = allEntries[:top]
	}

	return &graph.ImpactResult{
		Targets:    req.Paths,
		CoChanged:  allEntries,
		TotalFound: totalFound,
		QueryMs:    time.Since(start).Milliseconds(),
	}, nil
}

// sortImpactEntries ranks by aggregate score, then breadth of seed coupling,
// then total co-changes, then path for determinism.
func sortImpactEntries(entries []graph.ImpactEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.SeedMatches != b.SeedMatches {
			return a.SeedMatches > b.SeedMatches
		}
		if a.CouplingCount != b.CouplingCount {
			return a.CouplingCount > b.CouplingCount
		}
		return a.Path < b.Path
	})
}

func (r *SQLiteRepository) ResolveRenames(ctx context.Context, filePath string) ([]string, error) {
	visited := map[string]bool{filePath: true}
	queue := []string{filePath}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Find paths this file was renamed FROM.
		rows, err := r.db().QueryContext(ctx,
			"SELECT old_path FROM renames WHERE new_path = ?", current,
		)
		if err != nil {
			return nil, fmt.Errorf("query renames (new_path): %w", err)
		}
		for rows.Next() {
			var old string
			if err := rows.Scan(&old); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan old_path: %w", err)
			}
			if !visited[old] {
				visited[old] = true
				queue = append(queue, old)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("iterate renames (new_path): %w", err)
		}

		// Find paths this file was renamed TO.
		rows, err = r.db().QueryContext(ctx,
			"SELECT new_path FROM renames WHERE old_path = ?", current,
		)
		if err != nil {
			return nil, fmt.Errorf("query renames (old_path): %w", err)
		}
		for rows.Next() {
			var newP string
			if err := rows.Scan(&newP); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan new_path: %w", err)
			}
			if !visited[newP] {
				visited[newP] = true
				queue = append(queue, newP)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("iterate renames (old_path): %w", err)
		}
	}

	// Collect all paths except the original.
	var result []string
	for p := range visited {
		if p != filePath {
			result = append(result, p)
		}
	}
	return result, nil
}

func (r *SQLiteRepository) GetActiveSession(ctx context.Context, source, instanceID string, timeoutMinutes int) (*graph.SessionNode, error) {
	cutoff := time.Now().Unix() - int64(timeoutMinutes*60)
	var s graph.SessionNode
	var instanceNull sql.NullString
	err := r.db().QueryRowContext(ctx,
		`SELECT id, source, instance_id, started_at, ended_at FROM sessions
		 WHERE source = ? AND (instance_id = ? OR instance_id IS NULL)
		   AND ended_at = 0 AND started_at > ?
		 ORDER BY started_at DESC LIMIT 1`,
		source, instanceID, cutoff,
	).Scan(&s.ID, &s.Source, &instanceNull, &s.StartedAt, &s.EndedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if instanceNull.Valid {
		s.InstanceID = instanceNull.String
	}
	return &s, nil
}

func (r *SQLiteRepository) UpsertSession(ctx context.Context, s graph.SessionNode) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR REPLACE INTO sessions (id, source, instance_id, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?)`,
		s.ID, s.Source, s.InstanceID, s.StartedAt, s.EndedAt,
	)
	return err
}

func (r *SQLiteRepository) EndSession(ctx context.Context, sessionID string) error {
	_, err := r.db().ExecContext(ctx,
		`UPDATE sessions SET ended_at = ? WHERE id = ?`,
		time.Now().Unix(), sessionID,
	)
	return err
}

func (r *SQLiteRepository) CreateAction(ctx context.Context, a graph.ActionNode) error {
	filesJSON, err := json.Marshal(a.FilesChanged)
	if err != nil {
		return fmt.Errorf("marshal files_changed: %w", err)
	}
	_, err = r.db().ExecContext(ctx,
		`INSERT INTO actions (id, session_id, sequence, tool, diff, files_changed, timestamp, message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionID, a.Sequence, a.Tool, a.Diff, string(filesJSON), a.Timestamp, a.Message,
	)
	return err
}

func (r *SQLiteRepository) CreateActionBatch(ctx context.Context, a graph.ActionNode, modifiedFiles []graph.FileChange) (graph.ActionNode, error) {
	filesJSON, err := json.Marshal(a.FilesChanged)
	if err != nil {
		return graph.ActionNode{}, fmt.Errorf("marshal files_changed: %w", err)
	}

	tx, err := r.db().BeginTx(ctx, nil)
	if err != nil {
		return graph.ActionNode{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Derive the sequence and id atomically inside the write transaction. A
	// single INSERT...SELECT computes MAX(sequence)+1 under the writer lock, so
	// two concurrent captures on the same session serialize and the second sees
	// the first's row rather than re-deriving the same id.
	var seq int
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO actions (id, session_id, sequence, tool, diff, files_changed, timestamp, message)
		 SELECT printf('%s:%d', ?, seq), ?, seq, ?, ?, ?, ?, ?
		 FROM (SELECT COALESCE(MAX(sequence), 0) + 1 AS seq FROM actions WHERE session_id = ?)
		 RETURNING sequence`,
		a.SessionID, a.SessionID, a.Tool, a.Diff, string(filesJSON), a.Timestamp, a.Message, a.SessionID,
	).Scan(&seq); err != nil {
		return graph.ActionNode{}, fmt.Errorf("insert action: %w", err)
	}
	a.Sequence = seq
	a.ID = fmt.Sprintf("%s:%d", a.SessionID, seq)

	for _, f := range modifiedFiles {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO action_modifies (action_id, file_path, additions, deletions)
			 VALUES (?, ?, ?, ?)`,
			a.ID, f.Path, f.Additions, f.Deletions,
		); err != nil {
			return graph.ActionNode{}, fmt.Errorf("insert action_modifies: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return graph.ActionNode{}, fmt.Errorf("commit action batch: %w", err)
	}
	return a, nil
}

func (r *SQLiteRepository) GetActionCountForSession(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := r.db().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM actions WHERE session_id = ?`, sessionID,
	).Scan(&count)
	return count, err
}

func (r *SQLiteRepository) CreateActionModifies(ctx context.Context, actionID, filePath string, additions, deletions int) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO action_modifies (action_id, file_path, additions, deletions)
		 VALUES (?, ?, ?, ?)`,
		actionID, filePath, additions, deletions,
	)
	return err
}

func (r *SQLiteRepository) CreateActionProduces(ctx context.Context, actionID, commitHash, filePath string) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO action_produces (action_id, commit_hash, file_path)
		 VALUES (?, ?, ?)`,
		actionID, commitHash, filePath,
	)
	return err
}

func (r *SQLiteRepository) Timeline(ctx context.Context, req graph.TimelineRequest) (*graph.TimelineResult, error) {
	start := time.Now()

	top := req.Top
	if top <= 0 {
		top = 50
	}

	sinceTS := req.Since

	// When filtering by file, pre-select sessions that have matching actions
	// so the LIMIT applies to qualifying sessions, not all sessions.
	var query string
	var args []any
	if req.File != "" {
		query = `SELECT s.id, s.source, s.instance_id, s.started_at, s.ended_at
			FROM sessions s
			JOIN actions a ON a.session_id = s.id
			JOIN action_modifies am ON am.action_id = a.id
			WHERE (? = '' OR s.source = ?)
			  AND a.timestamp >= ?
			  AND am.file_path = ?
			GROUP BY s.id, s.source, s.instance_id, s.started_at, s.ended_at
			ORDER BY MAX(a.timestamp) DESC
			LIMIT ?`
		args = []any{req.Source, req.Source, sinceTS, req.File, top}
	} else {
		query = `SELECT s.id, s.source, s.instance_id, s.started_at, s.ended_at
			FROM sessions s
			JOIN actions a ON a.session_id = s.id
			WHERE (? = '' OR s.source = ?)
			  AND a.timestamp >= ?
			GROUP BY s.id, s.source, s.instance_id, s.started_at, s.ended_at
			ORDER BY MAX(a.timestamp) DESC
			LIMIT ?`
		args = []any{req.Source, req.Source, sinceTS, top}
	}

	rows, err := r.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []graph.TimelineSession
	totalActions := 0

	for rows.Next() {
		var id, source string
		var instanceID sql.NullString
		var startedAt, endedAt int64

		if err := rows.Scan(&id, &source, &instanceID, &startedAt, &endedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}

		sess := graph.TimelineSession{
			ID:        id,
			Source:    source,
			StartedAt: time.Unix(startedAt, 0).UTC().Format(time.RFC3339),
		}
		if endedAt > 0 {
			sess.EndedAt = time.Unix(endedAt, 0).UTC().Format(time.RFC3339)
		}

		actions, err := r.timelineActions(ctx, id, req.File, sinceTS)
		if err != nil {
			return nil, fmt.Errorf("query actions for session %s: %w", id, err)
		}

		sess.Actions = actions
		sess.ActionCount = len(actions)
		totalActions += len(actions)

		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return &graph.TimelineResult{
		Sessions:      sessions,
		TotalSessions: len(sessions),
		TotalActions:  totalActions,
		QueryMs:       time.Since(start).Milliseconds(),
	}, nil
}

func (r *SQLiteRepository) timelineActions(ctx context.Context, sessionID, fileFilter string, since int64) ([]graph.TimelineAction, error) {
	var rows *sql.Rows
	var err error

	if fileFilter != "" {
		rows, err = r.db().QueryContext(ctx,
			`SELECT a.id, a.tool, a.timestamp, a.files_changed
			 FROM actions a
			 WHERE a.session_id = ?
			   AND a.timestamp >= ?
			   AND EXISTS (
			     SELECT 1 FROM action_modifies am WHERE am.action_id = a.id AND am.file_path = ?
			   )
			 ORDER BY a.sequence`,
			sessionID, since, fileFilter,
		)
	} else {
		rows, err = r.db().QueryContext(ctx,
			`SELECT a.id, a.tool, a.timestamp, a.files_changed
			 FROM actions a
			 WHERE a.session_id = ?
			   AND a.timestamp >= ?
			 ORDER BY a.sequence`,
			sessionID, since,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []graph.TimelineAction
	for rows.Next() {
		var id, tool string
		var timestamp int64
		var filesJSON string

		if err := rows.Scan(&id, &tool, &timestamp, &filesJSON); err != nil {
			return nil, fmt.Errorf("scan action: %w", err)
		}

		var files []string
		if filesJSON != "" {
			if jsonErr := json.Unmarshal([]byte(filesJSON), &files); jsonErr != nil {
				// files_changed might not be valid JSON; treat as empty.
				files = nil
			}
		}

		actions = append(actions, graph.TimelineAction{
			ID:        id,
			Tool:      tool,
			Timestamp: time.Unix(timestamp, 0).UTC().Format(time.RFC3339),
			Files:     files,
		})
	}
	return actions, rows.Err()
}

func (r *SQLiteRepository) UnlinkedActionsForFiles(ctx context.Context, filePaths []string, since int64) ([]graph.ActionNode, error) {
	if len(filePaths) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(filePaths)-1) + "?"
	query := fmt.Sprintf(`
		SELECT DISTINCT a.id, a.session_id, a.sequence, a.tool, a.diff, a.files_changed, a.timestamp, a.message
		FROM actions a
		JOIN action_modifies am ON am.action_id = a.id
		LEFT JOIN action_produces ap ON ap.action_id = a.id AND ap.file_path = am.file_path
		WHERE ap.action_id IS NULL
		  AND a.timestamp >= ?
		  AND am.file_path IN (%s)
		ORDER BY a.timestamp`, placeholders)

	args := make([]any, 0, len(filePaths)+1)
	args = append(args, since)
	for _, f := range filePaths {
		args = append(args, f)
	}

	rows, err := r.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []graph.ActionNode
	for rows.Next() {
		var a graph.ActionNode
		var diffVal, filesJSON, msg sql.NullString
		var tool sql.NullString
		if err := rows.Scan(&a.ID, &a.SessionID, &a.Sequence, &tool, &diffVal, &filesJSON, &a.Timestamp, &msg); err != nil {
			return nil, err
		}
		if tool.Valid {
			a.Tool = tool.String
		}
		if diffVal.Valid {
			a.Diff = diffVal.String
		}
		if msg.Valid {
			a.Message = msg.String
		}
		if filesJSON.Valid {
			_ = json.Unmarshal([]byte(filesJSON.String), &a.FilesChanged)
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

// ResetProjections truncates the derived Projection tables so a rebuild can
// regenerate them from the Event Log. The append-only events table is never
// touched. Run inside one transaction so a failed rebuild can't leave half the
// Projections cleared.
func (r *SQLiteRepository) ResetProjections(ctx context.Context) error {
	tx, err := r.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reset projections tx: %w", err)
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`DELETE FROM event_files`,
		`DELETE FROM action_produces`,
		`DELETE FROM action_modifies`,
		`DELETE FROM actions`,
		`DELETE FROM sessions`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("reset projections: %w", err)
		}
	}
	return tx.Commit()
}

// CreateEventFile records one touched-file Projection row for an Event. The
// File Blob Refs are derived on the cold path; (event_seq, file_path) is the key.
func (r *SQLiteRepository) CreateEventFile(ctx context.Context, ef graph.EventFile) error {
	_, err := r.execStmt(ctx,
		`INSERT OR REPLACE INTO event_files
			(event_seq, file_path, before_blob, after_blob, change_kind, additions, deletions)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ef.EventSeq, ef.FilePath, nullableString(ef.BeforeBlob), nullableString(ef.AfterBlob),
		ef.ChangeKind, ef.Additions, ef.Deletions,
	)
	return err
}

// nullableString stores "" as SQL NULL so absent File Blob Refs read back as
// NULL rather than the empty string.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// LatestAfterBlob returns the most recent non-null after_blob recorded for
// filePath, i.e. the last content state the Event Log accounts for. ok is false
// when the log has never touched the path (or only recorded null blobs).
func (r *SQLiteRepository) LatestAfterBlob(ctx context.Context, filePath string) (string, bool, error) {
	var blob sql.NullString
	err := r.db().QueryRowContext(ctx,
		`SELECT after_blob FROM event_files
		 WHERE file_path = ? AND after_blob IS NOT NULL
		 ORDER BY event_seq DESC LIMIT 1`,
		filePath,
	).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("latest after_blob for %s: %w", filePath, err)
	}
	if !blob.Valid {
		return "", false, nil
	}
	return blob.String, true, nil
}

// FileChanges returns every event_files row whose file_path is in filePaths,
// joined to its events row, in ascending seq order. action_produces is joined by
// file_path to surface any linked commit. The read covers observed and
// out-of-band Events alike (they share the events table), so it backs both the
// Provenance View and the diagnose Candidate blob refs.
func (r *SQLiteRepository) FileChanges(ctx context.Context, filePaths []string) ([]graph.FileChangeRow, error) {
	if len(filePaths) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(filePaths))
	args := make([]any, len(filePaths))
	for i, p := range filePaths {
		placeholders[i] = "?"
		args[i] = p
	}

	query := `SELECT ef.event_seq, e.event_id, e.recorded_at, e.source, e.tool_name,
			ef.file_path, ef.before_blob, ef.after_blob, ef.change_kind,
			(SELECT ap.commit_hash FROM action_produces ap
			 JOIN actions a2 ON a2.id = ap.action_id
			 WHERE ap.file_path = ef.file_path
			 ORDER BY a2.timestamp DESC, ap.commit_hash DESC LIMIT 1) AS linked_commit
		 FROM event_files ef
		 JOIN events e ON e.seq = ef.event_seq
		 WHERE ef.file_path IN (` + strings.Join(placeholders, ",") + `)
		 ORDER BY ef.event_seq ASC`

	rows, err := r.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query file changes: %w", err)
	}
	defer rows.Close()

	var out []graph.FileChangeRow
	for rows.Next() {
		var (
			fc                              graph.FileChangeRow
			toolName, beforeBlob, afterBlob sql.NullString
			changeKind, linkedCommit        sql.NullString
		)
		if err := rows.Scan(
			&fc.Seq, &fc.EventID, &fc.RecordedAt, &fc.Source, &toolName,
			&fc.FilePath, &beforeBlob, &afterBlob, &changeKind, &linkedCommit,
		); err != nil {
			return nil, fmt.Errorf("scan file change: %w", err)
		}
		fc.ToolName = toolName.String
		fc.BeforeBlob = beforeBlob.String
		fc.AfterBlob = afterBlob.String
		fc.ChangeKind = changeKind.String
		fc.LinkedCommit = linkedCommit.String
		out = append(out, fc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file changes: %w", err)
	}
	return out, nil
}

// AppendEvent is the only writer into the events table. Read-head, hash, and
// insert run inside one BEGIN IMMEDIATE transaction so two concurrent writers
// cannot read the same head and fork the chain. seq is assigned explicitly from
// MAX(seq)+1 inside the txn (rather than left to AUTOINCREMENT) because seq is
// folded into the canonical form, so it must be known before the this_hash is
// computed and stored.
func (r *SQLiteRepository) AppendEvent(ctx context.Context, e graph.EventRecord) (graph.EventRecord, error) {
	tx, err := r.db().BeginTx(ctx, nil)
	if err != nil {
		if isBusyErr(err) {
			return graph.EventRecord{}, graph.ErrChainBusy
		}
		return graph.EventRecord{}, fmt.Errorf("begin append: %w", err)
	}
	defer tx.Rollback()

	var maxSeq sql.NullInt64
	var headHash sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq), (SELECT this_hash FROM events ORDER BY seq DESC LIMIT 1) FROM events`,
	).Scan(&maxSeq, &headHash); err != nil {
		if isBusyErr(err) {
			return graph.EventRecord{}, graph.ErrChainBusy
		}
		return graph.EventRecord{}, fmt.Errorf("read chain head: %w", err)
	}

	e.Seq = maxSeq.Int64 + 1
	if headHash.Valid {
		e.PrevHash = headHash.String
	} else {
		e.PrevHash = graph.GenesisHash
	}
	e.ThisHash = r.hasher.Hash(e.PrevHash, e)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events (
			seq, event_id, recorded_at, source, instance_id, kind,
			hook_event_name, tool_name, cwd, transcript_path, permission_mode,
			payload_raw, payload_size, truncated,
			command, exit_code, exit_code_source, is_test, is_build, test_name,
			prev_hash, this_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Seq, e.EventID, e.RecordedAt, string(e.Source), e.InstanceID, string(e.Kind),
		e.HookEventName, e.ToolName, e.Cwd, e.TranscriptPath, e.PermissionMode,
		string(e.PayloadRaw), e.PayloadSize, boolToInt(e.Truncated),
		e.Command, exitCodeArg(e.ExitCode), e.ExitCodeSource, boolToInt(e.IsTest), boolToInt(e.IsBuild), e.TestName,
		e.PrevHash, e.ThisHash,
	); err != nil {
		if isBusyErr(err) {
			return graph.EventRecord{}, graph.ErrChainBusy
		}
		return graph.EventRecord{}, fmt.Errorf("insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if isBusyErr(err) {
			return graph.EventRecord{}, graph.ErrChainBusy
		}
		return graph.EventRecord{}, fmt.Errorf("commit append: %w", err)
	}
	return e, nil
}

// HeadHash returns the current chain head (this_hash of the highest seq), or the
// Genesis value when the log is empty.
func (r *SQLiteRepository) HeadHash(ctx context.Context) (string, error) {
	var headHash sql.NullString
	if err := r.db().QueryRowContext(ctx,
		`SELECT this_hash FROM events ORDER BY seq DESC LIMIT 1`,
	).Scan(&headHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return graph.GenesisHash, nil
		}
		return "", fmt.Errorf("read head hash: %w", err)
	}
	if !headHash.Valid {
		return graph.GenesisHash, nil
	}
	return headHash.String, nil
}

// StreamEvents returns a cursor over events with seq > sinceSeq, in seq order.
// The query is read-only and safe under WAL alongside an active writer.
func (r *SQLiteRepository) StreamEvents(ctx context.Context, sinceSeq int64) (graph.EventCursor, error) {
	rows, err := r.db().QueryContext(ctx,
		`SELECT seq, event_id, recorded_at, source, instance_id, kind,
			hook_event_name, tool_name, cwd, transcript_path, permission_mode,
			payload_raw, payload_size, truncated,
			command, exit_code, exit_code_source, is_test, is_build, test_name,
			prev_hash, this_hash
		 FROM events WHERE seq > ? ORDER BY seq ASC`,
		sinceSeq,
	)
	if err != nil {
		return nil, fmt.Errorf("stream events: %w", err)
	}
	return &sqliteEventCursor{rows: rows}, nil
}

// sqliteEventCursor iterates a result set of events as graph.EventRecord values.
type sqliteEventCursor struct {
	rows *sql.Rows
	cur  graph.EventRecord
	err  error
}

func (c *sqliteEventCursor) Next() bool {
	if c.err != nil || !c.rows.Next() {
		return false
	}
	var (
		source, kind                        string
		instanceID, hookEventName, toolName sql.NullString
		cwd, transcriptPath, permissionMode sql.NullString
		command, exitCodeSource, testName   sql.NullString
		exitCode                            sql.NullInt64
		truncated, isTest, isBuild          int
		payloadRaw                          []byte
	)
	rec := graph.EventRecord{}
	if err := c.rows.Scan(
		&rec.Seq, &rec.EventID, &rec.RecordedAt, &source, &instanceID, &kind,
		&hookEventName, &toolName, &cwd, &transcriptPath, &permissionMode,
		&payloadRaw, &rec.PayloadSize, &truncated,
		&command, &exitCode, &exitCodeSource, &isTest, &isBuild, &testName,
		&rec.PrevHash, &rec.ThisHash,
	); err != nil {
		c.err = err
		return false
	}
	rec.Source = graph.EventSource(source)
	rec.Kind = graph.EventKind(kind)
	rec.InstanceID = instanceID.String
	rec.HookEventName = hookEventName.String
	rec.ToolName = toolName.String
	rec.Cwd = cwd.String
	rec.TranscriptPath = transcriptPath.String
	rec.PermissionMode = permissionMode.String
	rec.PayloadRaw = payloadRaw
	rec.Truncated = truncated != 0
	rec.Command = command.String
	if exitCode.Valid {
		code := int(exitCode.Int64)
		rec.ExitCode = &code
	}
	rec.ExitCodeSource = exitCodeSource.String
	rec.IsTest = isTest != 0
	rec.IsBuild = isBuild != 0
	rec.TestName = testName.String
	c.cur = rec
	return true
}

func (c *sqliteEventCursor) Event() graph.EventRecord { return c.cur }

func (c *sqliteEventCursor) Err() error {
	if c.err != nil {
		return c.err
	}
	return c.rows.Err()
}

func (c *sqliteEventCursor) Close() error { return c.rows.Close() }

func exitCodeArg(code *int) any {
	if code == nil {
		return nil
	}
	return *code
}

// VerifyChain walks events ORDER BY seq, recomputes each this_hash, follows the
// prev_hash linkage from Genesis, and checks seq continuity, then classifies the
// first integrity break. The classification precedence — inserted, deleted,
// edited, reordered — reflects which invariant fails first per the design's
// audit-surface table.
func (r *SQLiteRepository) VerifyChain(ctx context.Context) (graph.VerifyResult, error) {
	rows, err := r.loadChainRows(ctx)
	if err != nil {
		return graph.VerifyResult{}, err
	}
	total := int64(len(rows))
	if total == 0 {
		return graph.VerifyResult{Status: "ok"}, nil
	}

	// Self-hash recompute over each row's own (stored prev_hash, canonical fields).
	selfHashOK := make([]bool, len(rows))
	for i, e := range rows {
		selfHashOK[i] = r.hasher.Hash(e.PrevHash, e) == e.ThisHash
	}

	// Linkage walk from the Genesis row (prev_hash == Genesis), following each
	// row's this_hash to the next row whose prev_hash matches it.
	byPrev := make(map[string]int, len(rows))
	for i, e := range rows {
		byPrev[e.PrevHash] = i
	}
	reachable := make([]bool, len(rows))
	var linkageOrder []int
	for prev := graph.GenesisHash; ; {
		i, ok := byPrev[prev]
		if !ok || reachable[i] {
			break
		}
		reachable[i] = true
		linkageOrder = append(linkageOrder, i)
		prev = rows[i].ThisHash
	}

	// seq continuity: rows must run contiguously from the first row's seq.
	seqContiguous := true
	for i := 1; i < len(rows); i++ {
		if rows[i].Seq != rows[i-1].Seq+1 {
			seqContiguous = false
			break
		}
	}

	broken := func(idx int, kind graph.ChainBreakKind) graph.VerifyResult {
		e := rows[idx]
		return graph.VerifyResult{
			Status:         "broken",
			EventsTotal:    total,
			EventsVerified: int64(idx),
			FirstBreak: &graph.ChainBreak{
				Seq:              e.Seq,
				EventID:          e.EventID,
				Kind:             kind,
				ExpectedThisHash: r.hasher.Hash(e.PrevHash, e),
				StoredThisHash:   e.ThisHash,
			},
		}
	}

	// ROW_INSERTED: a row not reachable from the genesis walk while seq stays
	// contiguous (an extra row spliced in without breaking the seq run).
	if seqContiguous {
		for i := range rows {
			if !reachable[i] {
				return broken(i, graph.ChainBreakRowInserted), nil
			}
		}
	}

	// ROW_DELETED: a seq gap means a row was removed; the linkage walk terminates
	// early at the row preceding the gap.
	if !seqContiguous {
		for i := 1; i < len(rows); i++ {
			if rows[i].Seq != rows[i-1].Seq+1 {
				return broken(i-1, graph.ChainBreakRowDeleted), nil
			}
		}
	}

	// ROW_EDITED: a self-hash mismatch with linkage intact and seq contiguous.
	for i := range rows {
		if !selfHashOK[i] {
			return broken(i, graph.ChainBreakRowEdited), nil
		}
	}

	// ROW_REORDERED: every self-hash recomputes and every row is reachable, but
	// the linkage traversal visits rows in a different order than ascending seq.
	for pos, idx := range linkageOrder {
		if idx != pos {
			return broken(idx, graph.ChainBreakRowReordered), nil
		}
	}

	return graph.VerifyResult{
		Status:         "ok",
		EventsTotal:    total,
		EventsVerified: total,
	}, nil
}

// loadChainRows reads every Event in seq order for verification.
func (r *SQLiteRepository) loadChainRows(ctx context.Context) ([]graph.EventRecord, error) {
	cur, err := r.StreamEvents(ctx, 0)
	if err != nil {
		return nil, err
	}
	defer cur.Close()
	var rows []graph.EventRecord
	for cur.Next() {
		rows = append(rows, cur.Event())
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("verify chain read: %w", err)
	}
	return rows, nil
}
