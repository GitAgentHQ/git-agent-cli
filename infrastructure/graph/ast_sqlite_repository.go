package graph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitagenthq/git-agent/domain/graph"
)

var _ graph.ASTRepository = (*SQLiteASTRepository)(nil)

type SQLiteASTRepository struct {
	client *SQLiteClient
	tx     *sql.Tx
}

func NewSQLiteASTRepository(client *SQLiteClient) *SQLiteASTRepository {
	return &SQLiteASTRepository{client: client}
}

type astDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *SQLiteASTRepository) db() astDB {
	if r.tx != nil {
		return r.tx
	}
	return r.client.DB()
}

func (r *SQLiteASTRepository) RunInTx(ctx context.Context, fn func() error) error {
	if r.tx != nil {
		return fn()
	}
	tx, err := r.client.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ast tx: %w", err)
	}
	r.tx = tx
	defer func() { r.tx = nil }()

	if err := fn(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w; rollback ast tx: %v", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ast tx: %w", err)
	}
	return nil
}

func (r *SQLiteASTRepository) UpsertASTNode(ctx context.Context, n graph.ASTNode) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR REPLACE INTO ast_nodes (id, kind, name, qualified_name, file_path, language,
		 start_line, end_line, start_column, end_column, signature, visibility,
		 is_exported, is_async, is_static, is_abstract, return_type, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, string(n.Kind), n.Name, n.QualifiedName, n.FilePath, n.Language,
		n.StartLine, n.EndLine, n.StartColumn, n.EndColumn, n.Signature, n.Visibility,
		boolToInt(n.IsExported), boolToInt(n.IsAsync), boolToInt(n.IsStatic), boolToInt(n.IsAbstract),
		n.ReturnType, n.UpdatedAt,
	)
	return err
}

func (r *SQLiteASTRepository) UpsertASTEdge(ctx context.Context, e graph.ASTEdge) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO ast_edges (source, target, kind, line, column, provenance, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Source, e.Target, string(e.Kind), e.Line, e.Column, e.Provenance, e.Metadata,
	)
	return err
}

func (r *SQLiteASTRepository) UpsertUnresolvedRef(ctx context.Context, ref graph.ASTUnresolvedRef) error {
	_, err := r.db().ExecContext(ctx,
		`INSERT OR IGNORE INTO ast_unresolved_refs (from_node_id, reference_name, reference_kind, line, column, file_path, language, var_call_hint)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ref.FromNodeID, ref.ReferenceName, ref.ReferenceKind, ref.Line, ref.Column, ref.FilePath, ref.Language, ref.VarCallHint,
	)
	return err
}

func (r *SQLiteASTRepository) GetASTNodeByName(ctx context.Context, name string) ([]graph.ASTNode, error) {
	rows, err := r.db().QueryContext(ctx,
		`SELECT id, kind, name, qualified_name, file_path, language,
		 start_line, end_line, start_column, end_column, signature, visibility,
		 is_exported, is_async, is_static, is_abstract, return_type, updated_at
		 FROM ast_nodes WHERE lower(name) = lower(?)`, name,
	)
	if err != nil {
		return nil, fmt.Errorf("query ast_nodes by name: %w", err)
	}
	defer rows.Close()
	return scanASTNodes(rows)
}

func (r *SQLiteASTRepository) GetASTNodeByQualifiedName(ctx context.Context, qname string) (*graph.ASTNode, error) {
	var n graph.ASTNode
	err := r.db().QueryRowContext(ctx,
		`SELECT id, kind, name, qualified_name, file_path, language,
		 start_line, end_line, start_column, end_column, signature, visibility,
		 is_exported, is_async, is_static, is_abstract, return_type, updated_at
		 FROM ast_nodes WHERE qualified_name = ?`, qname,
	).Scan(&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath, &n.Language,
		&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn, &n.Signature, &n.Visibility,
		&n.IsExported, &n.IsAsync, &n.IsStatic, &n.IsAbstract, &n.ReturnType, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query ast_nodes by qname: %w", err)
	}
	return &n, nil
}

func (r *SQLiteASTRepository) GetCallers(ctx context.Context, nodeID string, maxDepth int) ([]graph.ASTImpactEntry, error) {
	return r.bfsIncoming(ctx, nodeID, maxDepth, []graph.ASTEdgeKind{graph.ASTEdgeKindCalls, graph.ASTEdgeKindReferences, graph.ASTEdgeKindInstantiates})
}

func (r *SQLiteASTRepository) GetCallees(ctx context.Context, nodeID string, maxDepth int) ([]graph.ASTImpactEntry, error) {
	return r.bfsOutgoing(ctx, nodeID, maxDepth, []graph.ASTEdgeKind{graph.ASTEdgeKindCalls, graph.ASTEdgeKindReferences, graph.ASTEdgeKindInstantiates})
}

// containerNodeKinds are the kinds whose members are worth surfacing when the
// node itself is the impact seed: an impact query on a class/struct/interface
// should surface callers of its methods, not just direct references to the type.
var containerNodeKinds = map[graph.ASTNodeKind]bool{
	graph.ASTNodeKindClass:     true,
	graph.ASTNodeKindStruct:    true,
	graph.ASTNodeKindInterface: true,
	graph.ASTNodeKindTrait:     true,
	graph.ASTNodeKindModule:    true,
	graph.ASTNodeKindEnum:      true,
	graph.ASTNodeKindNamespace: true,
}

// expandContainerChildren returns the members of the given container node IDs
// at the given depth, marking them visited and adding them to entries. It is
// the counterpart to codegraph's container expansion: impact on a type surfaces
// callers of its members. The expansion walks outgoing contains edges, plus
// extends/implements so methods promoted through Go struct/interface embedding
// (and Go-style type conformance generally) are reachable. contains/extends/
// implements are only ever walked outward (parent→member / type→supertype),
// never upward on the incoming traversal.
func (r *SQLiteASTRepository) expandContainerChildren(ctx context.Context, frontier []string, depth int, visited map[string]bool, entries *[]graph.ASTImpactEntry, kinds map[string]graph.ASTNodeKind) ([]string, error) {
	var containers []string
	for _, id := range frontier {
		if kind, ok := kinds[id]; ok && containerNodeKinds[kind] {
			containers = append(containers, id)
		}
	}
	if len(containers) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(containers)-1) + "?"
	args := make([]any, 0, len(containers))
	for _, c := range containers {
		args = append(args, c)
	}

	expansionKinds := []string{
		string(graph.ASTEdgeKindContains),
		string(graph.ASTEdgeKindExtends),
		string(graph.ASTEdgeKindImplements),
	}
	kindPlaceholders := strings.Repeat("?,", len(expansionKinds)-1) + "?"

	query := fmt.Sprintf(
		`SELECT e.source, e.target, e.kind, e.line, e.column, e.provenance, e.metadata,
		 n.id, n.kind, n.name, n.qualified_name, n.file_path, n.language,
		 n.start_line, n.end_line, n.start_column, n.end_column, n.signature, n.visibility,
		 n.is_exported, n.is_async, n.is_static, n.is_abstract, n.return_type, n.updated_at
		 FROM ast_edges e JOIN ast_nodes n ON e.target = n.id
		 WHERE e.source IN (%s) AND e.kind IN (%s)`,
		placeholders, kindPlaceholders,
	)
	for _, k := range expansionKinds {
		args = append(args, k)
	}

	rows, err := r.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("expand container children depth %d: %w", depth, err)
	}
	defer rows.Close()

	var added []string
	for rows.Next() {
		var edge graph.ASTEdge
		var node graph.ASTNode
		var edgeKind, nodeKind, provenance, metadata string
		var isExported, isAsync, isStatic, isAbstract int

		if err := rows.Scan(
			&edge.Source, &edge.Target, &edgeKind, &edge.Line, &edge.Column, &provenance, &metadata,
			&node.ID, &nodeKind, &node.Name, &node.QualifiedName, &node.FilePath, &node.Language,
			&node.StartLine, &node.EndLine, &node.StartColumn, &node.EndColumn, &node.Signature, &node.Visibility,
			&isExported, &isAsync, &isStatic, &isAbstract, &node.ReturnType, &node.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan container child: %w", err)
		}

		edge.Kind = graph.ASTEdgeKind(edgeKind)
		edge.Provenance = provenance
		edge.Metadata = metadata
		node.Kind = graph.ASTNodeKind(nodeKind)
		node.IsExported = isExported == 1
		node.IsAsync = isAsync == 1
		node.IsStatic = isStatic == 1
		node.IsAbstract = isAbstract == 1

		if !visited[node.ID] {
			visited[node.ID] = true
			kinds[node.ID] = node.Kind
			*entries = append(*entries, graph.ASTImpactEntry{
				Node:  node,
				Edge:  edge,
				Depth: depth,
			})
			added = append(added, node.ID)
		}
	}
	return added, rows.Err()
}

func (r *SQLiteASTRepository) GetImpactRadius(ctx context.Context, nodeID string, maxDepth int) (*graph.ASTImpactResult, error) {
	start := time.Now()

	seedNode, err := r.GetASTNodeByQualifiedNameOrID(ctx, nodeID)
	if err != nil || seedNode == nil {
		return nil, fmt.Errorf("find seed node %s: %w", nodeID, err)
	}

	impactKinds := []graph.ASTEdgeKind{graph.ASTEdgeKindCalls, graph.ASTEdgeKindReferences, graph.ASTEdgeKindImports, graph.ASTEdgeKindInstantiates, graph.ASTEdgeKindExtends, graph.ASTEdgeKindImplements}
	entries, err := r.bfsIncoming(ctx, nodeID, maxDepth, impactKinds)
	if err != nil {
		return nil, err
	}

	return &graph.ASTImpactResult{
		SeedNode:   *seedNode,
		Impacted:   entries,
		TotalFound: len(entries),
		QueryMs:    time.Since(start).Milliseconds(),
	}, nil
}

func (r *SQLiteASTRepository) SearchASTNodes(ctx context.Context, query string, kinds []graph.ASTNodeKind) ([]graph.ASTSearchResult, error) {
	var kindFilter string
	var kindArgs []any
	if len(kinds) > 0 {
		placeholders := strings.Repeat("?,", len(kinds)-1) + "?"
		for _, k := range kinds {
			kindArgs = append(kindArgs, string(k))
		}
		kindFilter = fmt.Sprintf(" AND n.kind IN (%s)", placeholders)
	}

	// FTS5 path: rank by relevance across name/qualified_name/signature. The
	// query is sanitized to a prefix token (name: foo*) so partial matches
	// rank higher and bare queries don't trigger FTS5 syntax errors.
	ftsExpr := sanitizeFTSPrefix(query)
	var rows *sql.Rows
	var err error
	if ftsExpr != "" {
		args := append([]any{ftsExpr}, kindArgs...)
		rows, err = r.db().QueryContext(ctx,
			fmt.Sprintf(`SELECT n.id, n.kind, n.name, n.qualified_name, n.file_path, n.language,
			 n.start_line, n.end_line, n.start_column, n.end_column, n.signature, n.visibility,
			 n.is_exported, n.is_async, n.is_static, n.is_abstract, n.return_type, n.updated_at,
			 bm25(ast_nodes_fts) AS score
			 FROM ast_nodes_fts JOIN ast_nodes n ON n.rowid = ast_nodes_fts.rowid
			 WHERE ast_nodes_fts MATCH ?%s
			 ORDER BY score LIMIT 50`, kindFilter),
			args...,
		)
		if err != nil {
			return nil, fmt.Errorf("search ast_nodes (fts): %w", err)
		}
	} else {
		args := append([]any{query}, kindArgs...)
		rows, err = r.db().QueryContext(ctx,
			fmt.Sprintf(`SELECT id, kind, name, qualified_name, file_path, language,
			 start_line, end_line, start_column, end_column, signature, visibility,
			 is_exported, is_async, is_static, is_abstract, return_type, updated_at, 0.0
			 FROM ast_nodes WHERE name LIKE ? || '%%'%s ORDER BY name LIMIT 50`, kindFilter),
			args...,
		)
		if err != nil {
			return nil, fmt.Errorf("search ast_nodes: %w", err)
		}
	}
	defer rows.Close()

	var nodes []graph.ASTNode
	var scores []float64
	for rows.Next() {
		var n graph.ASTNode
		var kind, visibility string
		var isExported, isAsync, isStatic, isAbstract int
		var score float64

		if err := rows.Scan(
			&n.ID, &kind, &n.Name, &n.QualifiedName, &n.FilePath, &n.Language,
			&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn, &n.Signature, &visibility,
			&isExported, &isAsync, &isStatic, &isAbstract, &n.ReturnType, &n.UpdatedAt, &score,
		); err != nil {
			return nil, fmt.Errorf("scan ast_node: %w", err)
		}
		n.Kind = graph.ASTNodeKind(kind)
		n.Visibility = visibility
		n.IsExported = isExported == 1
		n.IsAsync = isAsync == 1
		n.IsStatic = isStatic == 1
		n.IsAbstract = isAbstract == 1
		nodes = append(nodes, n)
		scores = append(scores, score)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]graph.ASTSearchResult, len(nodes))
	for i, n := range nodes {
		// bm25 returns negative scores (lower = better match). Normalize so a
		// perfect/exact name match scores highest; the LIKE fallback yields 0.
		s := scores[i]
		if s == 0 {
			s = 1.0
		} else {
			s = 1.0 / (1.0 - s) // s is negative; closer to 0 → stronger match
		}
		results[i] = graph.ASTSearchResult{Node: n, Score: s}
	}
	return results, nil
}

// sanitizeFTSPrefix turns a user query into a safe FTS5 prefix expression. It
// strips characters with FTS5 syntactic meaning and appends the prefix
// wildcard so "Co" becomes "co*", matching any name starting with "co".
// Returns "" if the query has no usable token (empty/all-punctuation).
func sanitizeFTSPrefix(query string) string {
	var b strings.Builder
	for _, r := range query {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '\t':
			// collapse whitespace to a single space boundary
			if b.Len() > 0 && b.String()[b.Len()-1] != ' ' {
				b.WriteRune(' ')
			}
		default:
			// drop punctuation; treat as token separator
			if b.Len() > 0 && b.String()[b.Len()-1] != ' ' {
				b.WriteRune(' ')
			}
		}
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return ""
	}
	// Quote each token and apply the prefix wildcard to the last one.
	tokens := strings.Fields(s)
	for i, t := range tokens {
		tokens[i] = "\"" + t + "\""
	}
	tokens[len(tokens)-1] = tokens[len(tokens)-1] + "*"
	return "name:" + strings.Join(tokens, " name:")
}

func (r *SQLiteASTRepository) DeleteASTNodesForFile(ctx context.Context, filePath string) error {
	_, err := r.db().ExecContext(ctx,
		`DELETE FROM ast_edges WHERE source IN (SELECT id FROM ast_nodes WHERE file_path = ?) OR target IN (SELECT id FROM ast_nodes WHERE file_path = ?)`,
		filePath, filePath,
	)
	if err != nil {
		return fmt.Errorf("delete ast_edges for file: %w", err)
	}
	_, err = r.db().ExecContext(ctx,
		`DELETE FROM ast_unresolved_refs WHERE file_path = ?`,
		filePath,
	)
	if err != nil {
		return fmt.Errorf("delete ast_unresolved_refs for file: %w", err)
	}
	_, err = r.db().ExecContext(ctx,
		`DELETE FROM ast_nodes WHERE file_path = ?`,
		filePath,
	)
	if err != nil {
		return fmt.Errorf("delete ast_nodes for file: %w", err)
	}
	return nil
}

func (r *SQLiteASTRepository) DeleteASTNodesExceptFiles(ctx context.Context, filePaths []string) error {
	if len(filePaths) == 0 {
		if _, err := r.db().ExecContext(ctx, `DELETE FROM ast_edges`); err != nil {
			return fmt.Errorf("delete all ast_edges: %w", err)
		}
		if _, err := r.db().ExecContext(ctx, `DELETE FROM ast_unresolved_refs`); err != nil {
			return fmt.Errorf("delete all ast_unresolved_refs: %w", err)
		}
		if _, err := r.db().ExecContext(ctx, `DELETE FROM ast_nodes`); err != nil {
			return fmt.Errorf("delete all ast_nodes: %w", err)
		}
		return nil
	}

	placeholders := strings.Repeat("?,", len(filePaths)-1) + "?"
	args := make([]any, len(filePaths))
	for i, p := range filePaths {
		args[i] = p
	}

	edgeArgs := append(append([]any{}, args...), args...)
	if _, err := r.db().ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM ast_edges
			WHERE source IN (SELECT id FROM ast_nodes WHERE file_path NOT IN (%s))
			   OR target IN (SELECT id FROM ast_nodes WHERE file_path NOT IN (%s))`, placeholders, placeholders),
		edgeArgs...,
	); err != nil {
		return fmt.Errorf("delete stale ast_edges: %w", err)
	}
	if _, err := r.db().ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM ast_unresolved_refs WHERE file_path NOT IN (%s)`, placeholders),
		args...,
	); err != nil {
		return fmt.Errorf("delete stale ast_unresolved_refs: %w", err)
	}
	if _, err := r.db().ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM ast_nodes WHERE file_path NOT IN (%s)`, placeholders),
		args...,
	); err != nil {
		return fmt.Errorf("delete stale ast_nodes: %w", err)
	}
	return nil
}

func (r *SQLiteASTRepository) ListUnresolvedRefs(ctx context.Context) ([]graph.ASTUnresolvedRef, error) {
	rows, err := r.db().QueryContext(ctx,
		`SELECT from_node_id, reference_name, reference_kind, line, column, file_path, language, var_call_hint
		 FROM ast_unresolved_refs`)
	if err != nil {
		return nil, fmt.Errorf("list unresolved refs: %w", err)
	}
	defer rows.Close()

	var refs []graph.ASTUnresolvedRef
	for rows.Next() {
		var ref graph.ASTUnresolvedRef
		if err := rows.Scan(
			&ref.FromNodeID, &ref.ReferenceName, &ref.ReferenceKind,
			&ref.Line, &ref.Column, &ref.FilePath, &ref.Language, &ref.VarCallHint,
		); err != nil {
			return nil, fmt.Errorf("scan unresolved ref: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (r *SQLiteASTRepository) ListUnresolvedRefsMatching(ctx context.Context, filePaths []string, lookupNames []string) ([]graph.ASTUnresolvedRef, error) {
	if len(filePaths) == 0 && len(lookupNames) == 0 {
		return r.ListUnresolvedRefs(ctx)
	}

	var conds []string
	var args []any

	if len(filePaths) > 0 {
		placeholders := strings.Repeat("?,", len(filePaths)-1) + "?"
		conds = append(conds, fmt.Sprintf("file_path IN (%s)", placeholders))
		for _, p := range filePaths {
			args = append(args, p)
		}
	}
	for _, name := range lookupNames {
		if name == "" {
			continue
		}
		conds = append(conds, "(lower(reference_name) = lower(?) OR lower(reference_name) LIKE lower(?))")
		args = append(args, name, "%."+name)
	}
	if len(conds) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(
		`SELECT from_node_id, reference_name, reference_kind, line, column, file_path, language, var_call_hint
		 FROM ast_unresolved_refs WHERE %s`,
		strings.Join(conds, " OR "),
	)
	rows, err := r.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list unresolved refs matching: %w", err)
	}
	defer rows.Close()

	var refs []graph.ASTUnresolvedRef
	for rows.Next() {
		var ref graph.ASTUnresolvedRef
		if err := rows.Scan(
			&ref.FromNodeID, &ref.ReferenceName, &ref.ReferenceKind,
			&ref.Line, &ref.Column, &ref.FilePath, &ref.Language, &ref.VarCallHint,
		); err != nil {
			return nil, fmt.Errorf("scan unresolved ref: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (r *SQLiteASTRepository) ListASTNodeNames(ctx context.Context) ([]string, error) {
	rows, err := r.db().QueryContext(ctx, `SELECT DISTINCT name FROM ast_nodes`)
	if err != nil {
		return nil, fmt.Errorf("list ast node names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan ast node name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (r *SQLiteASTRepository) GetASTNodeByQualifiedNameOrID(ctx context.Context, id string) (*graph.ASTNode, error) {
	n, err := r.GetASTNodeByQualifiedName(ctx, id)
	if err != nil {
		return nil, err
	}
	if n != nil {
		return n, nil
	}

	var node graph.ASTNode
	err = r.db().QueryRowContext(ctx,
		`SELECT id, kind, name, qualified_name, file_path, language,
		 start_line, end_line, start_column, end_column, signature, visibility,
		 is_exported, is_async, is_static, is_abstract, return_type, updated_at
		 FROM ast_nodes WHERE id = ?`, id,
	).Scan(&node.ID, &node.Kind, &node.Name, &node.QualifiedName, &node.FilePath, &node.Language,
		&node.StartLine, &node.EndLine, &node.StartColumn, &node.EndColumn, &node.Signature, &node.Visibility,
		&node.IsExported, &node.IsAsync, &node.IsStatic, &node.IsAbstract, &node.ReturnType, &node.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query ast_nodes by id: %w", err)
	}
	return &node, nil
}

func (r *SQLiteASTRepository) bfsIncoming(ctx context.Context, startID string, maxDepth int, edgeKinds []graph.ASTEdgeKind) ([]graph.ASTImpactEntry, error) {
	kindPlaceholders := strings.Repeat("?,", len(edgeKinds)-1) + "?"
	kindArgs := make([]any, len(edgeKinds))
	for i, k := range edgeKinds {
		kindArgs[i] = string(k)
	}

	visited := map[string]bool{startID: true}
	kinds := map[string]graph.ASTNodeKind{}
	if err := r.seedNodeKind(ctx, startID, kinds); err != nil {
		return nil, err
	}
	var entries []graph.ASTImpactEntry
	frontier := []string{startID}

	for d := 1; d <= maxDepth; d++ {
		if len(frontier) == 0 {
			break
		}

		// Container expansion runs at the START of each depth: surface a
		// container's own members (via contains/extends/implements edges) at the
		// same depth so impact on a class/struct/interface surfaces its methods'
		// dependents, including methods promoted through embedding. The added
		// nodes also join nextFrontier so their own members expand next depth.
		// Contains/extends/implements are only ever walked outward, never upward.
		var expanded []string
		if added, err := r.expandContainerChildren(ctx, frontier, d, visited, &entries, kinds); err != nil {
			return nil, err
		} else {
			frontier = append(frontier, added...)
			expanded = added
		}
		if len(frontier) == 0 {
			break
		}

		placeholders := strings.Repeat("?,", len(frontier)-1) + "?"
		args := make([]any, 0, len(frontier)+len(edgeKinds))
		for _, f := range frontier {
			args = append(args, f)
		}
		args = append(args, kindArgs...)

		query := fmt.Sprintf(
			`SELECT e.source, e.target, e.kind, e.line, e.column, e.provenance, e.metadata,
			 n.id, n.kind, n.name, n.qualified_name, n.file_path, n.language,
			 n.start_line, n.end_line, n.start_column, n.end_column, n.signature, n.visibility,
			 n.is_exported, n.is_async, n.is_static, n.is_abstract, n.return_type, n.updated_at
			 FROM ast_edges e JOIN ast_nodes n ON e.source = n.id
			 WHERE e.target IN (%s) AND e.kind IN (%s)
			 ORDER BY CASE e.kind
			     WHEN 'calls' THEN 0
			     WHEN 'instantiates' THEN 1
			     WHEN 'references' THEN 2
			     ELSE 3 END, n.name`,
			placeholders, kindPlaceholders,
		)

		rows, err := r.db().QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("bfs incoming depth %d: %w", d, err)
		}

		// Container nodes reached via expansion this depth continue into the
		// next depth so their members expand (e.g. an embedded type's methods).
		nextFrontier := append([]string{}, expanded...)
		for rows.Next() {
			var edge graph.ASTEdge
			var node graph.ASTNode
			var edgeKind, nodeKind, provenance, metadata string
			var isExported, isAsync, isStatic, isAbstract int

			if err := rows.Scan(
				&edge.Source, &edge.Target, &edgeKind, &edge.Line, &edge.Column, &provenance, &metadata,
				&node.ID, &nodeKind, &node.Name, &node.QualifiedName, &node.FilePath, &node.Language,
				&node.StartLine, &node.EndLine, &node.StartColumn, &node.EndColumn, &node.Signature, &node.Visibility,
				&isExported, &isAsync, &isStatic, &isAbstract, &node.ReturnType, &node.UpdatedAt,
			); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan bfs incoming: %w", err)
			}

			edge.Kind = graph.ASTEdgeKind(edgeKind)
			edge.Provenance = provenance
			edge.Metadata = metadata
			node.Kind = graph.ASTNodeKind(nodeKind)
			node.IsExported = isExported == 1
			node.IsAsync = isAsync == 1
			node.IsStatic = isStatic == 1
			node.IsAbstract = isAbstract == 1

			if !visited[node.ID] {
				visited[node.ID] = true
				kinds[node.ID] = node.Kind
				entries = append(entries, graph.ASTImpactEntry{
					Node:  node,
					Edge:  edge,
					Depth: d,
				})
				nextFrontier = append(nextFrontier, node.ID)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate bfs incoming: %w", err)
		}
		rows.Close()

		frontier = nextFrontier
	}

	return entries, nil
}

func (r *SQLiteASTRepository) bfsOutgoing(ctx context.Context, startID string, maxDepth int, edgeKinds []graph.ASTEdgeKind) ([]graph.ASTImpactEntry, error) {
	kindPlaceholders := strings.Repeat("?,", len(edgeKinds)-1) + "?"
	kindArgs := make([]any, len(edgeKinds))
	for i, k := range edgeKinds {
		kindArgs[i] = string(k)
	}

	visited := map[string]bool{startID: true}
	kinds := map[string]graph.ASTNodeKind{}
	if err := r.seedNodeKind(ctx, startID, kinds); err != nil {
		return nil, err
	}
	var entries []graph.ASTImpactEntry
	frontier := []string{startID}

	for d := 1; d <= maxDepth; d++ {
		if len(frontier) == 0 {
			break
		}

		// Container expansion at the start of each depth (see bfsIncoming).
		var expanded []string
		if added, err := r.expandContainerChildren(ctx, frontier, d, visited, &entries, kinds); err != nil {
			return nil, err
		} else {
			frontier = append(frontier, added...)
			expanded = added
		}
		if len(frontier) == 0 {
			break
		}

		placeholders := strings.Repeat("?,", len(frontier)-1) + "?"
		args := make([]any, 0, len(frontier)+len(edgeKinds))
		for _, f := range frontier {
			args = append(args, f)
		}
		args = append(args, kindArgs...)

		query := fmt.Sprintf(
			`SELECT e.source, e.target, e.kind, e.line, e.column, e.provenance, e.metadata,
			 n.id, n.kind, n.name, n.qualified_name, n.file_path, n.language,
			 n.start_line, n.end_line, n.start_column, n.end_column, n.signature, n.visibility,
			 n.is_exported, n.is_async, n.is_static, n.is_abstract, n.return_type, n.updated_at
			 FROM ast_edges e JOIN ast_nodes n ON e.target = n.id
			 WHERE e.source IN (%s) AND e.kind IN (%s)
			 ORDER BY CASE e.kind
			     WHEN 'calls' THEN 0
			     WHEN 'instantiates' THEN 1
			     WHEN 'references' THEN 2
			     ELSE 3 END, n.name`,
			placeholders, kindPlaceholders,
		)

		rows, err := r.db().QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("bfs outgoing depth %d: %w", d, err)
		}

		nextFrontier := append([]string{}, expanded...)
		for rows.Next() {
			var edge graph.ASTEdge
			var node graph.ASTNode
			var edgeKind, nodeKind, provenance, metadata string
			var isExported, isAsync, isStatic, isAbstract int

			if err := rows.Scan(
				&edge.Source, &edge.Target, &edgeKind, &edge.Line, &edge.Column, &provenance, &metadata,
				&node.ID, &nodeKind, &node.Name, &node.QualifiedName, &node.FilePath, &node.Language,
				&node.StartLine, &node.EndLine, &node.StartColumn, &node.EndColumn, &node.Signature, &node.Visibility,
				&isExported, &isAsync, &isStatic, &isAbstract, &node.ReturnType, &node.UpdatedAt,
			); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan bfs outgoing: %w", err)
			}

			edge.Kind = graph.ASTEdgeKind(edgeKind)
			edge.Provenance = provenance
			edge.Metadata = metadata
			node.Kind = graph.ASTNodeKind(nodeKind)
			node.IsExported = isExported == 1
			node.IsAsync = isAsync == 1
			node.IsStatic = isStatic == 1
			node.IsAbstract = isAbstract == 1

			if !visited[node.ID] {
				visited[node.ID] = true
				kinds[node.ID] = node.Kind
				entries = append(entries, graph.ASTImpactEntry{
					Node:  node,
					Edge:  edge,
					Depth: d,
				})
				nextFrontier = append(nextFrontier, node.ID)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate bfs outgoing: %w", err)
		}
		rows.Close()

		frontier = nextFrontier
	}

	return entries, nil
}

// seedNodeKind loads the start node's kind so container expansion can apply
// to a container that is the BFS seed (depth 0) itself.
func (r *SQLiteASTRepository) seedNodeKind(ctx context.Context, startID string, kinds map[string]graph.ASTNodeKind) error {
	var kind string
	err := r.db().QueryRowContext(ctx, `SELECT kind FROM ast_nodes WHERE id = ?`, startID).Scan(&kind)
	if err == sql.ErrNoRows {
		return nil // node may be absent in edge-only contexts; expansion simply skips
	}
	if err != nil {
		return fmt.Errorf("seed node kind: %w", err)
	}
	kinds[startID] = graph.ASTNodeKind(kind)
	return nil
}

func scanASTNodes(rows *sql.Rows) ([]graph.ASTNode, error) {
	var nodes []graph.ASTNode
	for rows.Next() {
		var n graph.ASTNode
		var kind, visibility string
		var isExported, isAsync, isStatic, isAbstract int

		if err := rows.Scan(
			&n.ID, &kind, &n.Name, &n.QualifiedName, &n.FilePath, &n.Language,
			&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn, &n.Signature, &visibility,
			&isExported, &isAsync, &isStatic, &isAbstract, &n.ReturnType, &n.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ast_node: %w", err)
		}

		n.Kind = graph.ASTNodeKind(kind)
		n.Visibility = visibility
		n.IsExported = isExported == 1
		n.IsAsync = isAsync == 1
		n.IsStatic = isStatic == 1
		n.IsAbstract = isAbstract == 1

		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
