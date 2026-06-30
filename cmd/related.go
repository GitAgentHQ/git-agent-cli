package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var relatedCmd = &cobra.Command{
	Use:   "related [path...]",
	Short: "Show files that change together with the given files (co-change)",
	Long: `Find the code related to a set of files by mining git history for
co-change: which files are habitually modified in the same commits, and the
commits that prove it (subject + sha + date). Seeds may be one or more files or
directories; with no arguments, the current working-tree changes are used —
"given what I've edited, what else usually changes, and why?". Files coupled to
several seeds rank highest. Language-agnostic (history, not parsing), offline,
no API key. Auto-indexes git history on first run. Read-only.

Use --tests to keep only the related test files (which tests to run after a
change). In -o json (the piped default) each related file carries a "commits"
array of the commits that link it to the seeds.`,
	Args: cobra.ArbitraryArgs,
	RunE: jsonAwareRunE(runRelated),
}

func runRelated(cmd *cobra.Command, args []string) error {
	depth, _ := cmd.Flags().GetInt("depth")
	top, _ := cmd.Flags().GetInt("top")
	minCount, _ := cmd.Flags().GetInt("min-count")
	reindex, _ := cmd.Flags().GetBool("reindex")
	testsOnly, _ := cmd.Flags().GetBool("tests")

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

	graphGit := infraGit.NewGraphClient(root)

	// Resolve the seed set: explicit paths/dirs, or — with no args — the files
	// the user is currently changing ("what else moves with my edits?").
	if err := rejectOutsideRepoArgs(root, cwd, args); err != nil {
		return err
	}
	seeds, err := resolveSeeds(ctx, args, root, cwd, graphGit)
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		// Honor the agent JSON contract: emit a valid empty result on stdout
		// (matching the shape of a legitimately empty query) instead of nothing,
		// so a pipe into jq never hits a parse error on the no-seeds branch.
		if outputFormat(cmd) == output.FormatJSON {
			return output.EncodeJSON(cmd.OutOrStdout(), &graph.ImpactResult{Targets: args})
		}
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
		fmt.Fprintf(cmd.ErrOrStderr(), "Indexed %d commits [%dms]\n", indexResult.NewCommits, indexResult.DurationMs)
	}

	impactSvc := application.NewImpactService(repo)
	result, err := impactSvc.Impact(ctx, graph.ImpactRequest{
		Paths:          seeds,
		Depth:          depth,
		Top:            top,
		MinCount:       minCount,
		IncludeCommits: true,
	})
	if err != nil {
		return err
	}

	if testsOnly {
		filterToTests(result)
	}

	return outputResult(cmd, result)
}

// filterToTests narrows a result to its related test files (language-agnostic
// naming heuristic), so an agent gets "which tests should I run for this change".
func filterToTests(result *graph.ImpactResult) {
	if result == nil {
		return
	}
	kept := result.CoChanged[:0]
	for _, e := range result.CoChanged {
		if graph.IsTestFile(e.Path) {
			kept = append(kept, e)
		}
	}
	result.CoChanged = kept
	result.TotalFound = len(kept)
}

func openGraphDB(ctx context.Context, root string) (string, *infraGraph.SQLiteClient, error) {
	dbPath := infraGraph.DBPath(root)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return "", nil, fmt.Errorf("create .git-agent dir: %w", err)
	}
	// Defend the "first write before init" path: ensure .gitignore carries the
	// mandatory ignore rules so a later `git add -A` cannot track graph.db.
	if err := application.EnsureGitAgentIgnoredAt(root); err != nil {
		return "", nil, fmt.Errorf("ensure gitignore: %w", err)
	}
	// Untrack graph.db if a prior commit tracked it, so the loop breaks even
	// without init. Idempotent no-op when already untracked.
	if _, err := ensureGraphDBUntracked(ctx, infraGit.NewClient(), root); err != nil {
		return "", nil, err
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

func outputResult(cmd *cobra.Command, result *graph.ImpactResult) error {
	if outputFormat(cmd) == output.FormatJSON {
		return output.EncodeJSON(cmd.OutOrStdout(), result)
	}
	return outputText(cmd, result)
}

func outputText(cmd *cobra.Command, result *graph.ImpactResult) error {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Related to %s:\n\n", summarizeTargets(result.Targets))

	if len(result.CoChanged) == 0 {
		fmt.Fprintf(out, "  (no co-changed files found)\n\n")
		fmt.Fprintf(out, "0 related files found | query: %dms\n", result.QueryMs)
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
		// Show the commits that link this file to the seeds — the evidence for
		// "why is this related?". Capped to keep the listing scannable.
		for i, c := range e.LinkingCommits {
			if i >= 2 {
				break
			}
			fmt.Fprintf(out, "      ↳ %s  %s\n", shortSHA(c.Hash), c.Subject)
		}
	}

	fmt.Fprintf(out, "\n%d related files found | query: %dms\n", result.TotalFound, result.QueryMs)
	return nil
}

// shortSHA abbreviates a commit hash for human-facing output.
func shortSHA(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
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

// rejectOutsideRepoArgs fails fast (exit 1) when an explicit path argument
// resolves outside the repository root. Such a path has no graph data and never
// will; silently returning an empty result would be indistinguishable from a
// legitimately empty answer. In-repo paths that do not exist on disk are still
// accepted — they support querying the history of a since-deleted file.
func rejectOutsideRepoArgs(root, cwd string, args []string) error {
	for _, arg := range args {
		rel := normalizeRepoPath(root, cwd, arg)
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return fmt.Errorf("path %q resolves outside the repository", arg)
		}
	}
	return nil
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

func init() {
	relatedCmd.Flags().Int("depth", 1, "transitive co-change depth")
	relatedCmd.Flags().Int("top", 20, "max results")
	relatedCmd.Flags().Int("min-count", 3, "minimum co-change count")
	relatedCmd.Flags().Bool("reindex", false, "force full re-index before query")
	relatedCmd.Flags().Bool("tests", false, "show only related test files (which tests to run)")
	addOutputFlag(relatedCmd, false)
	rootCmd.AddCommand(relatedCmd)
}
