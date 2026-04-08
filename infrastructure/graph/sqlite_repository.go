package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// Compile-time check that SQLiteRepository satisfies GraphRepository.
var _ graph.GraphRepository = (*SQLiteRepository)(nil)

// SQLiteRepository implements graph.GraphRepository using a SQLiteClient.
type SQLiteRepository struct {
	client *SQLiteClient
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

// --- Indexing writes ---

func (r *SQLiteRepository) UpsertCommit(ctx context.Context, c graph.CommitNode) error {
	parentsJSON, err := json.Marshal(c.ParentHashes)
	if err != nil {
		return fmt.Errorf("marshal parent_hashes: %w", err)
	}
	_, err = r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO commits (hash, message, author_name, author_email, timestamp, parent_hashes)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.Hash, c.Message, c.AuthorName, c.AuthorEmail, c.Timestamp, string(parentsJSON),
	)
	return err
}

func (r *SQLiteRepository) UpsertAuthor(ctx context.Context, a graph.AuthorNode) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR REPLACE INTO authors (email, name) VALUES (?, ?)`,
		a.Email, a.Name,
	)
	return err
}

func (r *SQLiteRepository) UpsertFile(ctx context.Context, f graph.FileNode) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO files (path) VALUES (?)`,
		f.Path,
	)
	return err
}

func (r *SQLiteRepository) CreateModifies(ctx context.Context, e graph.ModifiesEdge) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO modifies (commit_hash, file_path, additions, deletions, status)
		 VALUES (?, ?, ?, ?, ?)`,
		e.CommitHash, e.FilePath, e.Additions, e.Deletions, e.Status,
	)
	return err
}

func (r *SQLiteRepository) CreateAuthored(ctx context.Context, authorEmail, commitHash string) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO authored (author_email, commit_hash) VALUES (?, ?)`,
		authorEmail, commitHash,
	)
	return err
}

func (r *SQLiteRepository) CreateRename(ctx context.Context, oldPath, newPath, commitHash string) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO renames (old_path, new_path, commit_hash) VALUES (?, ?, ?)`,
		oldPath, newPath, commitHash,
	)
	return err
}

// --- Index state ---

func (r *SQLiteRepository) GetLastIndexedCommit(ctx context.Context) (string, error) {
	var val sql.NullString
	err := r.db().QueryRowContext(ctx,
		`SELECT value FROM index_state WHERE key = ?`, "last_indexed_commit",
	).Scan(&val)
	if err == sql.ErrNoRows || !val.Valid {
		return "", nil
	}
	return val.String, err
}

func (r *SQLiteRepository) SetLastIndexedCommit(ctx context.Context, hash string) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR REPLACE INTO index_state (key, value) VALUES (?, ?)`,
		"last_indexed_commit", hash,
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

// --- Not yet implemented ---

func (r *SQLiteRepository) RecomputeCoChanged(ctx context.Context, minCount, maxFilesPerCommit int) error {
	tx, err := r.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM co_changed"); err != nil {
		return fmt.Errorf("delete co_changed: %w", err)
	}

	query := `
INSERT INTO co_changed (file_a, file_b, coupling_count, coupling_strength, last_coupled_hash)
WITH file_commit_counts AS (
    SELECT file_path, COUNT(DISTINCT commit_hash) AS total
    FROM modifies GROUP BY file_path
),
valid_commits AS (
    SELECT commit_hash FROM modifies
    GROUP BY commit_hash HAVING COUNT(*) <= ?
)
SELECT
    m1.file_path AS file_a,
    m2.file_path AS file_b,
    COUNT(DISTINCT m1.commit_hash) AS coupling_count,
    CAST(COUNT(DISTINCT m1.commit_hash) AS REAL) / MAX(fc1.total, fc2.total) AS coupling_strength,
    MAX(m1.commit_hash) AS last_coupled_hash
FROM modifies m1
JOIN modifies m2 ON m1.commit_hash = m2.commit_hash AND m1.file_path < m2.file_path
JOIN valid_commits vc ON vc.commit_hash = m1.commit_hash
JOIN file_commit_counts fc1 ON fc1.file_path = m1.file_path
JOIN file_commit_counts fc2 ON fc2.file_path = m2.file_path
GROUP BY m1.file_path, m2.file_path
HAVING COUNT(DISTINCT m1.commit_hash) >= ?`

	if _, err := tx.ExecContext(ctx, query, maxFilesPerCommit, minCount); err != nil {
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

	insertQuery := fmt.Sprintf(`
INSERT INTO co_changed (file_a, file_b, coupling_count, coupling_strength, last_coupled_hash)
WITH file_commit_counts AS (
    SELECT file_path, COUNT(DISTINCT commit_hash) AS total
    FROM modifies GROUP BY file_path
),
valid_commits AS (
    SELECT commit_hash FROM modifies
    GROUP BY commit_hash HAVING COUNT(*) <= ?
)
SELECT
    m1.file_path AS file_a,
    m2.file_path AS file_b,
    COUNT(DISTINCT m1.commit_hash) AS coupling_count,
    CAST(COUNT(DISTINCT m1.commit_hash) AS REAL) / MAX(fc1.total, fc2.total) AS coupling_strength,
    MAX(m1.commit_hash) AS last_coupled_hash
FROM modifies m1
JOIN modifies m2 ON m1.commit_hash = m2.commit_hash AND m1.file_path < m2.file_path
JOIN valid_commits vc ON vc.commit_hash = m1.commit_hash
JOIN file_commit_counts fc1 ON fc1.file_path = m1.file_path
JOIN file_commit_counts fc2 ON fc2.file_path = m2.file_path
WHERE m1.file_path IN (%s) OR m2.file_path IN (%s)
GROUP BY m1.file_path, m2.file_path
HAVING COUNT(DISTINCT m1.commit_hash) >= ?`,
		placeholders, placeholders,
	)

	if _, err := tx.ExecContext(ctx, insertQuery, insertArgs...); err != nil {
		return fmt.Errorf("insert incremental co_changed: %w", err)
	}

	return tx.Commit()
}

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

	// Resolve all historical aliases for the target path.
	aliases, err := r.ResolveRenames(ctx, req.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve renames: %w", err)
	}
	allPaths := append([]string{req.Path}, aliases...)

	// BFS across depth levels.
	visited := make(map[string]bool)
	for _, p := range allPaths {
		visited[p] = true
	}

	// Collect all results across all depths.
	var allEntries []graph.ImpactEntry

	// Current frontier: the set of paths to query at this depth level.
	frontier := allPaths

	for d := 1; d <= depth; d++ {
		if len(frontier) == 0 {
			break
		}

		// best tracks the highest-strength entry per neighbor path at this depth.
		best := make(map[string]*graph.ImpactEntry)

		for _, p := range frontier {
			rows, err := r.db().QueryContext(ctx,
				`SELECT
					CASE WHEN cc.file_a = ? THEN cc.file_b ELSE cc.file_a END AS neighbor,
					cc.coupling_count,
					cc.coupling_strength
				FROM co_changed cc
				WHERE (cc.file_a = ? OR cc.file_b = ?)
				  AND cc.coupling_count >= ?
				ORDER BY cc.coupling_strength DESC`,
				p, p, p, minCount,
			)
			if err != nil {
				return nil, fmt.Errorf("query co_changed for %q: %w", p, err)
			}
			for rows.Next() {
				var neighbor string
				var count int
				var strength float64
				if err := rows.Scan(&neighbor, &count, &strength); err != nil {
					rows.Close()
					return nil, fmt.Errorf("scan co_changed: %w", err)
				}
				if visited[neighbor] {
					continue
				}
				if existing, ok := best[neighbor]; !ok || strength > existing.CouplingStrength {
					best[neighbor] = &graph.ImpactEntry{
						Path:             neighbor,
						CouplingCount:    count,
						CouplingStrength: strength,
						Depth:            d,
					}
				}
			}
			rows.Close()
		}

		// Collect entries from this depth level and build next frontier.
		var nextFrontier []string
		for path, entry := range best {
			allEntries = append(allEntries, *entry)
			visited[path] = true
			nextFrontier = append(nextFrontier, path)
		}
		frontier = nextFrontier
	}

	totalFound := len(allEntries)

	// Sort by coupling_strength descending.
	sortImpactEntries(allEntries)

	// Apply top limit.
	if len(allEntries) > top {
		allEntries = allEntries[:top]
	}

	return &graph.ImpactResult{
		Target:     req.Path,
		CoChanged:  allEntries,
		TotalFound: totalFound,
		QueryMs:    time.Since(start).Milliseconds(),
	}, nil
}

// sortImpactEntries sorts entries by CouplingStrength descending.
func sortImpactEntries(entries []graph.ImpactEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].CouplingStrength > entries[j-1].CouplingStrength; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
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
		rows.Close()

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
		rows.Close()
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

func (r *SQLiteRepository) CreateActionProduces(ctx context.Context, actionID, commitHash string) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO action_produces (action_id, commit_hash)
		 VALUES (?, ?)`,
		actionID, commitHash,
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

	rows, err := r.db().QueryContext(ctx,
		`SELECT id, source, instance_id, started_at, ended_at FROM sessions
		 WHERE (? = '' OR source = ?)
		   AND started_at >= ?
		 ORDER BY started_at DESC
		 LIMIT ?`,
		req.Source, req.Source, sinceTS, top,
	)
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

		actions, err := r.timelineActions(ctx, id, req.File)
		if err != nil {
			return nil, fmt.Errorf("query actions for session %s: %w", id, err)
		}

		sess.Actions = actions
		sess.ActionCount = len(actions)
		totalActions += len(actions)

		// If filtering by file, skip sessions with no matching actions.
		if req.File != "" && len(actions) == 0 {
			continue
		}

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

func (r *SQLiteRepository) timelineActions(ctx context.Context, sessionID, fileFilter string) ([]graph.TimelineAction, error) {
	var rows *sql.Rows
	var err error

	if fileFilter != "" {
		rows, err = r.db().QueryContext(ctx,
			`SELECT a.id, a.tool, a.timestamp, a.files_changed
			 FROM actions a
			 WHERE a.session_id = ?
			   AND EXISTS (
			     SELECT 1 FROM action_modifies am WHERE am.action_id = a.id AND am.file_path = ?
			   )
			 ORDER BY a.sequence`,
			sessionID, fileFilter,
		)
	} else {
		rows, err = r.db().QueryContext(ctx,
			`SELECT a.id, a.tool, a.timestamp, a.files_changed
			 FROM actions a
			 WHERE a.session_id = ?
			 ORDER BY a.sequence`,
			sessionID,
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
		LEFT JOIN action_produces ap ON ap.action_id = a.id
		WHERE ap.action_id IS NULL
		  AND a.timestamp >= ?
		  AND am.file_path IN (%s)
		ORDER BY a.timestamp`, placeholders)

	args := make([]any, 0, len(filePaths)+1)
	args = append(args, since)
	for _, f := range filePaths {
		args = append(args, f)
	}

	rows, err := r.client.db.QueryContext(ctx, query, args...)
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

func (r *SQLiteRepository) GetCaptureBaseline(ctx context.Context, filePaths []string) (map[string]string, error) {
	if len(filePaths) == 0 {
		return map[string]string{}, nil
	}
	placeholders := make([]string, len(filePaths))
	args := make([]any, len(filePaths))
	for i, p := range filePaths {
		placeholders[i] = "?"
		args[i] = p
	}
	query := fmt.Sprintf(
		`SELECT file_path, content_hash FROM capture_baseline WHERE file_path IN (%s)`,
		strings.Join(placeholders, ","),
	)
	rows, err := r.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, err
		}
		result[path] = hash
	}
	return result, rows.Err()
}

func (r *SQLiteRepository) UpdateCaptureBaseline(ctx context.Context, updates map[string]string) error {
	now := time.Now().Unix()
	for path, hash := range updates {
		_, err := r.db().ExecContext(ctx,
			`INSERT OR REPLACE INTO capture_baseline (file_path, content_hash, captured_at)
			 VALUES (?, ?, ?)`,
			path, hash, now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLiteRepository) CleanupCaptureBaseline(ctx context.Context, currentFiles []string, olderThan int64) error {
	if len(currentFiles) == 0 {
		_, err := r.db().ExecContext(ctx,
			`DELETE FROM capture_baseline WHERE captured_at < ?`, olderThan,
		)
		return err
	}
	placeholders := make([]string, len(currentFiles))
	args := make([]any, len(currentFiles))
	for i, f := range currentFiles {
		placeholders[i] = "?"
		args[i] = f
	}
	args = append(args, olderThan)
	query := fmt.Sprintf(
		`DELETE FROM capture_baseline WHERE file_path NOT IN (%s) AND captured_at < ?`,
		strings.Join(placeholders, ","),
	)
	_, err := r.db().ExecContext(ctx, query, args...)
	return err
}
