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

// boolToInt maps a Go bool to the 0/1 integer SQLite stores for boolean columns.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// coChangeHalfLifeDays is the recency half-life for co-change weighting: a
// co-change this many days old contributes half the strength of a fresh one.
const coChangeHalfLifeDays = 365

// SQLiteRepository implements graph.GraphRepository using a SQLiteClient.
type SQLiteRepository struct {
	client    *SQLiteClient
	tx        *sql.Tx              // non-nil while a RunInTx batch is open
	stmtCache map[string]*sql.Stmt // prepared statements reused within the batch
}

// NewSQLiteRepository returns a new SQLiteRepository wrapping the given client.
func NewSQLiteRepository(client *SQLiteClient) *SQLiteRepository {
	return &SQLiteRepository{client: client}
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
	}

	for _, t := range tables {
		err := r.db().QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", t.name),
		).Scan(t.dest)
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", t.name, err)
		}
	}

	// DB file size via page_count * page_size (WAL-aware: main file pages).
	var pageCount, pageSize int64
	if err := r.db().QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount); err == nil {
		if err := r.db().QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err == nil {
			stats.DBSizeBytes = pageCount * pageSize
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

// LinkingCommits returns the commits that modified both seed and related,
// most-recent first, capped at limit. These are the commits that built the
// co-change coupling between the pair — the "why are these related?" evidence
// (subject is the first line of the commit message).
func (r *SQLiteRepository) LinkingCommits(ctx context.Context, seed, related string, limit int) ([]graph.CommitRef, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := r.db().QueryContext(ctx,
		`SELECT c.hash, c.message, c.timestamp
		FROM modifies m1
		JOIN modifies m2 ON m1.commit_hash = m2.commit_hash
		JOIN commits  c  ON c.hash = m1.commit_hash
		WHERE m1.file_path = ? AND m2.file_path = ?
		ORDER BY c.timestamp DESC
		LIMIT ?`,
		seed, related, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query linking commits for %q<->%q: %w", seed, related, err)
	}
	defer rows.Close()

	var out []graph.CommitRef
	for rows.Next() {
		var hash, message string
		var ts int64
		if err := rows.Scan(&hash, &message, &ts); err != nil {
			return nil, fmt.Errorf("scan linking commit: %w", err)
		}
		subject := message
		if i := strings.IndexByte(subject, '\n'); i >= 0 {
			subject = subject[:i]
		}
		out = append(out, graph.CommitRef{Hash: hash, Subject: strings.TrimSpace(subject), Timestamp: ts})
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
