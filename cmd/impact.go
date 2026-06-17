package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/graph"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
)

var impactCmd = &cobra.Command{
	Use:   "impact <path>",
	Short: "Show files affected by changing a target path",
	Long:  "Analyze co-change patterns to show which files are typically modified together with the target. Auto-indexes git history on first run.",
	Args:  cobra.ExactArgs(1),
	RunE:  runImpact,
}

func runImpact(cmd *cobra.Command, args []string) error {
	depth, _ := cmd.Flags().GetInt("depth")
	top, _ := cmd.Flags().GetInt("top")
	minCount, _ := cmd.Flags().GetInt("min-count")
	reindex, _ := cmd.Flags().GetBool("reindex")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")

	ctx := cmd.Context()

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	// The graph stores repo-relative paths. Accept whatever form the caller
	// passes — absolute, ./-prefixed, or relative to a subdirectory — and
	// resolve it the way git resolves a pathspec: against the current dir.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	targetPath := normalizeRepoPath(root, cwd, args[0])

	dbPath := filepath.Join(root, ".git-agent", "graph.db")

	// Ensure .git-agent directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create .git-agent dir: %w", err)
	}

	client := infraGraph.NewSQLiteClient(dbPath)
	repo := infraGraph.NewSQLiteRepository(client)
	if err := repo.Open(ctx); err != nil {
		return fmt.Errorf("open graph db: %w", err)
	}
	defer repo.Close()
	if err := repo.InitSchema(ctx); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	graphGit := infraGit.NewGraphClient(root)
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
		Path:     targetPath,
		Depth:    depth,
		Top:      top,
		MinCount: minCount,
	})
	if err != nil {
		return outputError(jsonFlag, textFlag, err)
	}

	return outputResult(cmd, result, jsonFlag, textFlag)
}

func outputError(jsonFlag, textFlag bool, err error) error {
	if useJSON(jsonFlag, textFlag) {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(map[string]string{"error": err.Error()})
		return nil
	}
	return err
}

func outputResult(cmd *cobra.Command, result *graph.ImpactResult, jsonFlag, textFlag bool) error {
	if useJSON(jsonFlag, textFlag) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	return outputText(cmd, result)
}

func outputText(cmd *cobra.Command, result *graph.ImpactResult) error {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Impact of %s:\n\n", result.Target)

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

	for _, e := range result.CoChanged {
		padding := strings.Repeat(" ", maxLen-len(e.Path)+2)
		pct := int(e.CouplingStrength * 100)
		fmt.Fprintf(out, "  %s%s%3d%%  (%d co-changes)\n", e.Path, padding, pct, e.CouplingCount)
	}

	fmt.Fprintf(out, "\n%d co-changed files found | query: %dms\n", result.TotalFound, result.QueryMs)
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

func useJSON(jsonFlag, textFlag bool) bool {
	if jsonFlag {
		return true
	}
	if textFlag {
		return false
	}
	return !isatty.IsTerminal(os.Stdout.Fd())
}

func init() {
	impactCmd.Flags().Int("depth", 1, "transitive co-change depth")
	impactCmd.Flags().Int("top", 20, "max results")
	impactCmd.Flags().Int("min-count", 3, "minimum co-change count")
	impactCmd.Flags().Bool("reindex", false, "force full re-index before query")
	impactCmd.Flags().Bool("json", false, "force JSON output")
	impactCmd.Flags().Bool("text", false, "force text output")
	impactCmd.MarkFlagsMutuallyExclusive("json", "text")

	rootCmd.AddCommand(impactCmd)
}
