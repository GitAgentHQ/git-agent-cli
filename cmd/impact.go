package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infraExtraction "github.com/gitagenthq/git-agent/infrastructure/extraction"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var impactCmd = &cobra.Command{
	Use:   "impact [path...]",
	Short: "Show files likely to change with a feature",
	Long: `Analyze co-change patterns to show which files are typically modified
together with the given seeds. Seeds may be one or more files or directories;
with no arguments, the current working-tree changes are used as seeds — "given
what I've edited, what else usually changes?". Files coupled to several seeds
rank highest. Auto-indexes git history on first run.`,
	Args: cobra.ArbitraryArgs,
	RunE: runImpact,
}

func runImpact(cmd *cobra.Command, args []string) error {
	depth, _ := cmd.Flags().GetInt("depth")
	top, _ := cmd.Flags().GetInt("top")
	minCount, _ := cmd.Flags().GetInt("min-count")
	reindex, _ := cmd.Flags().GetBool("reindex")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
	symbol, _ := cmd.Flags().GetString("symbol")
	mode, _ := cmd.Flags().GetString("mode")

	ctx := cmd.Context()

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	// Mode dispatch:
	//   --symbol with no explicit --mode (or --mode structural) → AST only
	//   --symbol with --mode combined → co-change of symbol's file + AST
	//   --symbol with --mode cochange → co-change of symbol's file
	//   --mode structural/combined without --symbol → error (need a symbol)
	//   everything else → existing co-change behavior
	switch {
	case symbol != "" && (mode == "" || mode == "structural"):
		return runASTImpact(cmd, ctx, root, symbol, depth, "structural", reindex, jsonFlag, textFlag)
	case symbol != "" && mode == "combined":
		return runASTImpact(cmd, ctx, root, symbol, depth, "combined", reindex, jsonFlag, textFlag)
	case symbol != "" && mode == "cochange":
		return runSymbolCoChangeImpact(cmd, ctx, root, symbol, depth, top, minCount, reindex, jsonFlag, textFlag)
	case symbol == "" && (mode == "structural" || mode == "combined"):
		return fmt.Errorf("--mode %s requires --symbol", mode)
	}

	graphGit := infraGit.NewGraphClient(root)

	// Resolve the seed set: explicit paths/dirs, or — with no args — the files
	// the user is currently changing ("what else moves with my edits?").
	seeds, err := resolveSeeds(ctx, args, root, cwd, graphGit)
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		if len(args) == 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "No working-tree changes to analyze. Pass one or more files or directories.")
		} else {
			fmt.Fprintln(cmd.ErrOrStderr(), "No graph-tracked files matched the given paths.")
		}
		return nil
	}

	dbPath, client, err := openGraphDB(ctx, root)
	if err != nil {
		return err
	}
	repo := infraGraph.NewSQLiteRepository(client)
	defer client.Close()

	indexSvc := application.NewIndexService(repo, graphGit)
	ensureIndexSvc := application.NewEnsureIndexService(indexSvc, repo, graphGit, dbPath)

	indexReq := graph.IndexRequest{Force: reindex, MaxFilesPerCommit: 50}
	indexResult, err := ensureIndexSvc.EnsureIndex(ctx, indexReq)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	if indexResult != nil && indexResult.NewCommits > 0 {
		fmt.Fprintf(os.Stderr, "Indexed %d commits [%dms]\n", indexResult.NewCommits, indexResult.DurationMs)
	}

	impactSvc := application.NewImpactService(repo)
	result, err := impactSvc.Impact(ctx, graph.ImpactRequest{
		Paths:    seeds,
		Depth:    depth,
		Top:      top,
		MinCount: minCount,
	})
	if err != nil {
		return outputError(jsonFlag, textFlag, err)
	}

	return outputResult(cmd, result, jsonFlag, textFlag)
}

// runASTImpact handles --symbol / --mode structural|combined.
func runASTImpact(cmd *cobra.Command, ctx context.Context, root, symbol string, depth int, mode string, forceIndex, jsonFlag, textFlag bool) error {
	dbPath, client, err := openGraphDB(ctx, root)
	if err != nil {
		return err
	}
	defer client.Close()

	astRepo := infraGraph.NewSQLiteASTRepository(client)
	stateRepo := infraGraph.NewSQLiteRepository(client)
	graphGit := infraGit.NewGraphClient(root)

	if err := ensureASTIndexForSymbol(ctx, root, astRepo, stateRepo, graphGit, symbol, forceIndex, cmd.ErrOrStderr()); err != nil {
		return outputError(jsonFlag, textFlag, err)
	}

	impactSvc := application.NewASTImpactService(astRepo)
	astResult, err := impactSvc.ImpactBySymbol(ctx, symbol, depth)
	if err != nil {
		return outputError(jsonFlag, textFlag, err)
	}

	// Combined mode: also run co-change for the seed symbol's file.
	if mode == "combined" {
		repo := infraGraph.NewSQLiteRepository(client)
		coChangeResult, err := runCoChangeWithRepo(ctx, root, graph.ImpactRequest{
			Paths: []string{astResult.SeedNode.FilePath},
			Top:   10,
		}, dbPath, repo)
		if err != nil {
			return outputError(jsonFlag, textFlag, err)
		}
		if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
			return outputCombinedJSON(cmd.OutOrStdout(), astResult, coChangeResult)
		}
		outputCombinedText(cmd.OutOrStdout(), astResult, coChangeResult)
		return nil
	}

	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return outputASTImpactJSON(cmd.OutOrStdout(), astResult)
	}
	outputASTImpactText(cmd.OutOrStdout(), astResult)
	return nil
}

func runSymbolCoChangeImpact(cmd *cobra.Command, ctx context.Context, root, symbol string, depth, top, minCount int, forceIndex, jsonFlag, textFlag bool) error {
	dbPath, client, err := openGraphDB(ctx, root)
	if err != nil {
		return err
	}
	defer client.Close()

	astRepo := infraGraph.NewSQLiteASTRepository(client)
	stateRepo := infraGraph.NewSQLiteRepository(client)
	graphGit := infraGit.NewGraphClient(root)
	if err := ensureASTIndexForSymbol(ctx, root, astRepo, stateRepo, graphGit, symbol, forceIndex, cmd.ErrOrStderr()); err != nil {
		return outputError(jsonFlag, textFlag, err)
	}

	astResult, err := application.NewASTImpactService(astRepo).ImpactBySymbol(ctx, symbol, 1)
	if err != nil {
		return outputError(jsonFlag, textFlag, err)
	}
	result, err := runCoChangeWithRepo(ctx, root, graph.ImpactRequest{
		Paths:    []string{astResult.SeedNode.FilePath},
		Depth:    depth,
		Top:      top,
		MinCount: minCount,
	}, dbPath, infraGraph.NewSQLiteRepository(client))
	if err != nil {
		return outputError(jsonFlag, textFlag, err)
	}
	return outputResult(cmd, result, jsonFlag, textFlag)
}

// runCoChangeForSymbol runs a co-change query for a symbol's resolved file path.
func runCoChangeForSymbol(ctx context.Context, root string, req graph.ImpactRequest) (*graph.ImpactResult, error) {
	dbPath, client, err := openGraphDB(ctx, root)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return runCoChangeWithRepo(ctx, root, req, dbPath, infraGraph.NewSQLiteRepository(client))
}

func runCoChangeWithRepo(ctx context.Context, root string, req graph.ImpactRequest, dbPath string, repo graph.GraphRepository) (*graph.ImpactResult, error) {
	if len(req.Paths) == 0 || req.Paths[0] == "" {
		return nil, fmt.Errorf("symbol file path is empty")
	}

	graphGit := infraGit.NewGraphClient(root)
	indexSvc := application.NewIndexService(repo, graphGit)
	ensureSvc := application.NewEnsureIndexService(indexSvc, repo, graphGit, dbPath)
	if _, err := ensureSvc.EnsureIndex(ctx, graph.IndexRequest{MaxFilesPerCommit: 50}); err != nil {
		return nil, err
	}

	impactSvc := application.NewImpactService(repo)
	result, err := impactSvc.Impact(ctx, req)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func openGraphDB(ctx context.Context, root string) (string, *infraGraph.SQLiteClient, error) {
	dbPath := filepath.Join(root, ".git-agent", "graph.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return "", nil, fmt.Errorf("create .git-agent dir: %w", err)
	}
	client := infraGraph.NewSQLiteClient(dbPath)
	if err := client.Open(ctx); err != nil {
		return "", nil, fmt.Errorf("open graph db: %w", err)
	}
	if err := client.ValidateSchemaVersion(ctx); err != nil {
		client.Close()
		return "", nil, err
	}
	if err := client.InitSchema(ctx); err != nil {
		client.Close()
		return "", nil, fmt.Errorf("init schema: %w", err)
	}
	return dbPath, client, nil
}

func ensureASTIndexForSymbol(ctx context.Context, root string, astRepo graph.ASTRepository, stateRepo application.ASTIndexStateRepository, graphGit *infraGit.GraphClient, symbol string, force bool, progress io.Writer) error {
	extractor := infraExtraction.NewTreeSitterExtractor("go", infraExtraction.GoExtractor())
	return application.NewASTEnsureIndexService(astRepo, stateRepo, graphGit, extractor).
		EnsureForSymbol(ctx, root, symbol, force, progress)
}

// ensureASTIndexAll brings the AST index up to date for queries that are not
// scoped to a single symbol (graph query, graph node by name).
func ensureASTIndexAll(ctx context.Context, root string, astRepo graph.ASTRepository, stateRepo application.ASTIndexStateRepository, graphGit *infraGit.GraphClient, force bool, progress io.Writer) error {
	extractor := infraExtraction.NewTreeSitterExtractor("go", infraExtraction.GoExtractor())
	return application.NewASTEnsureIndexService(astRepo, stateRepo, graphGit, extractor).
		EnsureAll(ctx, root, force, progress)
}

// resolveSeeds turns CLI arguments into repo-relative seed files. With no args,
// the working-tree changes are used. Each arg is normalized (git pathspec
// semantics); a directory expands to the git-tracked files beneath it.
func resolveSeeds(ctx context.Context, args []string, root, cwd string, graphGit *infraGit.GraphClient) ([]string, error) {
	seen := make(map[string]bool)
	var seeds []string
	add := func(p string) {
		if p != "" && !graph.IsToolingPath(p) && !seen[p] {
			seen[p] = true
			seeds = append(seeds, p)
		}
	}

	if len(args) == 0 {
		changed, err := graphGit.DiffNameOnly(ctx)
		if err != nil {
			return nil, fmt.Errorf("list working-tree changes: %w", err)
		}
		for _, f := range changed {
			add(f)
		}
		return seeds, nil
	}

	for _, arg := range args {
		rel := normalizeRepoPath(root, cwd, arg)
		if info, err := os.Stat(filepath.Join(root, rel)); err == nil && info.IsDir() {
			files, err := graphGit.TrackedFiles(ctx, rel)
			if err != nil {
				return nil, fmt.Errorf("expand directory %q: %w", arg, err)
			}
			for _, f := range files {
				add(f)
			}
			continue
		}
		add(rel)
	}
	return seeds, nil
}

func outputError(jsonFlag, textFlag bool, err error) error {
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		_ = output.EncodeJSON(os.Stdout, map[string]string{"error": err.Error()})
	}
	return err
}

func outputResult(cmd *cobra.Command, result *graph.ImpactResult, jsonFlag, textFlag bool) error {
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(cmd.OutOrStdout(), result)
	}
	return outputText(cmd, result)
}

func outputText(cmd *cobra.Command, result *graph.ImpactResult) error {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Impact of %s:\n\n", summarizeTargets(result.Targets))

	if len(result.CoChanged) == 0 {
		fmt.Fprintf(out, "  (no co-changed files found)\n\n")
		fmt.Fprintf(out, "0 co-changed files found | query: %dms\n", result.QueryMs)
		return nil
	}

	// Find the longest path for alignment.
	maxLen := 0
	for _, e := range result.CoChanged {
		if len(e.Path) > maxLen {
			maxLen = len(e.Path)
		}
	}

	totalSeeds := len(result.Targets)
	for _, e := range result.CoChanged {
		fmt.Fprintln(out, formatImpactLine(e, maxLen, totalSeeds))
	}

	fmt.Fprintf(out, "\n%d co-changed files found | query: %dms\n", result.TotalFound, result.QueryMs)
	return nil
}

// formatImpactLine renders one co-change row. With multiple seeds it shows how
// many of them the file is coupled to (breadth across the feature). Entries
// reached transitively (depth > 1) are marked so an indirect coupling is never
// misread as a direct one — the percentage shown is the strength of the last
// hop, not of a direct seed-to-file link.
func formatImpactLine(e graph.ImpactEntry, maxLen, totalSeeds int) string {
	padding := strings.Repeat(" ", maxLen-len(e.Path)+2)
	pct := int(e.CouplingStrength * 100)
	line := fmt.Sprintf("  %s%s%3d%%  (%d co-changes)", e.Path, padding, pct, e.CouplingCount)
	if totalSeeds > 1 && e.Depth <= 1 {
		line += fmt.Sprintf("  [%d/%d seeds: %s]", e.SeedMatches, totalSeeds, capJoin(e.RelatedTo, 4))
	}
	if e.Depth > 1 {
		line += fmt.Sprintf("  [indirect, depth %d]", e.Depth)
	}
	return line
}

// capJoin joins up to max items with ", " and appends " +N" for the remainder,
// so long seed lists stay readable (a directory can expand to 100+ seeds).
func capJoin(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:max], ", ") + fmt.Sprintf(" +%d", len(items)-max)
}

// summarizeTargets renders the seed set for the header, bounded so a directory
// expanding to many files does not print a wall of paths.
func summarizeTargets(targets []string) string {
	if len(targets) <= 3 {
		return strings.Join(targets, ", ")
	}
	return fmt.Sprintf("%s +%d more (%d seeds)", strings.Join(targets[:3], ", "), len(targets)-3, len(targets))
}

// normalizeRepoPath converts a user-supplied path into the repo-relative form
// the graph stores. It resolves the target against cwd (git pathspec semantics)
// and rebases onto the repo root, resolving symlinks on both sides so that a
// caller-supplied /tmp/... path matches a root that git reports as /private/tmp
// (the macOS case). Paths that resolve outside the repo are returned cleaned
// but otherwise untouched.
func normalizeRepoPath(root, cwd, target string) string {
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, target)
	}
	abs = realPath(abs)
	root = realPath(root)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Clean(target)
	}
	return filepath.ToSlash(rel)
}

// realPath canonicalizes p by resolving symlinks. The target file may not exist
// (e.g. querying a deleted file's history), so it falls back to resolving the
// longest existing ancestor and re-appending the remainder.
func realPath(p string) string {
	if rp, err := filepath.EvalSymlinks(p); err == nil {
		return rp
	}
	dir, base := filepath.Split(filepath.Clean(p))
	dir = filepath.Clean(dir)
	if dir == "" || dir == p {
		return p
	}
	if rp, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(rp, base)
	}
	parent := realPath(dir)
	if parent == dir {
		return p
	}
	return filepath.Join(parent, base)
}

// outputASTImpactJSON renders the structural impact result as JSON.
func outputASTImpactJSON(w io.Writer, result *graph.ASTImpactResult) error {
	return output.EncodeJSON(w, result)
}

// outputASTImpactText renders the structural impact result as human-readable text.
func outputASTImpactText(w io.Writer, result *graph.ASTImpactResult) {
	fmt.Fprintf(w, "Structural impact of %s (%s):\n\n", result.SeedNode.Name, result.SeedNode.Kind)

	if len(result.Impacted) == 0 {
		fmt.Fprintln(w, "  (no callers or references found)")
		fmt.Fprintf(w, "\n0 impacted symbols | query: %dms\n", result.QueryMs)
		return
	}

	// Split production callers (first) from test callers (last) so signal is
	// not buried under test noise. Entries keep their original order within
	// each group.
	var prod, tests []graph.ASTImpactEntry
	for _, e := range result.Impacted {
		if isTestFile(e.Node.FilePath) || isTestSymbol(e.Node.Name) {
			tests = append(tests, e)
		} else {
			prod = append(prod, e)
		}
	}

	renderImpactEntries(w, prod)
	if len(tests) > 0 {
		fmt.Fprintf(w, "\n  -- tests (%d) --\n", len(tests))
		renderImpactEntries(w, tests)
	}

	fmt.Fprintf(w, "\n%d impacted symbols | query: %dms\n", result.TotalFound, result.QueryMs)
}

func renderImpactEntries(w io.Writer, entries []graph.ASTImpactEntry) {
	if len(entries) == 0 {
		return
	}
	maxLen := 0
	for _, e := range entries {
		if len(e.Node.Name) > maxLen {
			maxLen = len(e.Node.Name)
		}
	}
	for _, e := range entries {
		padding := strings.Repeat(" ", maxLen-len(e.Node.Name)+2)
		line := fmt.Sprintf("  %s%s%s  %s  d%d", e.Node.Name, padding, e.Node.Kind, e.Node.FilePath, e.Depth)
		if e.Depth > 1 {
			line += "  [indirect]"
		}
		fmt.Fprintln(w, line)
	}
}

// isTestFile reports whether a Go file path is a test file.
func isTestFile(filePath string) bool {
	return strings.HasSuffix(filePath, "_test.go")
}

// isTestSymbol reports whether a symbol name looks like a Go test function/benchmark.
func isTestSymbol(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Example")
}

func init() {
	impactCmd.Flags().Int("depth", 1, "transitive co-change depth")
	impactCmd.Flags().Int("top", 20, "max results")
	impactCmd.Flags().Int("min-count", 3, "minimum co-change count")
	impactCmd.Flags().Bool("reindex", false, "force full re-index before query")
	impactCmd.Flags().Bool("json", false, "force JSON output")
	impactCmd.Flags().Bool("text", false, "force text output")
	impactCmd.Flags().String("symbol", "", "query structural impact by Go symbol name (tree-sitter, Go only)")
	impactCmd.Flags().String("mode", "", "impact mode: structural, combined, or cochange (default: cochange, or structural if --symbol given)")
	impactCmd.MarkFlagsMutuallyExclusive("json", "text")

	graphCmd.AddCommand(impactCmd)
}

// outputCombinedJSON renders both co-change and structural results as JSON.
func outputCombinedJSON(w io.Writer, astResult *graph.ASTImpactResult, ccResult *graph.ImpactResult) error {
	return output.EncodeJSON(w, map[string]any{
		"mode":           "combined",
		"structural":     astResult,
		"co_change":      ccResult,
		"seed_symbol":    astResult.SeedNode.Name,
		"seed_file":      astResult.SeedNode.FilePath,
		"struct_total":   astResult.TotalFound,
		"cochange_total": ccResult.TotalFound,
	})
}

// outputCombinedText renders both results in text format.
func outputCombinedText(w io.Writer, astResult *graph.ASTImpactResult, ccResult *graph.ImpactResult) {
	// Section 1: Structural impact
	outputASTImpactText(w, astResult)

	// Section 2: Co-change impact
	fmt.Fprintf(w, "\n--- Co-change for %s ---\n", astResult.SeedNode.FilePath)
	if len(ccResult.CoChanged) == 0 {
		fmt.Fprintln(w, "  (no co-changed files found)")
		return
	}
	for _, e := range ccResult.CoChanged {
		pct := int(e.CouplingStrength * 100)
		fmt.Fprintf(w, "  %-40s %3d%%  (%d co-changes)\n", e.Path, pct, e.CouplingCount)
	}
	fmt.Fprintf(w, "\n%d co-changed files | %d structural impacts\n",
		ccResult.TotalFound, astResult.TotalFound)
}
