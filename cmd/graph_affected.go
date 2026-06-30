package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	"github.com/gitagenthq/git-agent/pkg/output"
)

var graphAffectedCmd = &cobra.Command{
	Use:   "affected [files...]",
	Short: "Show test files affected by changes to the given files",
	Long: `Trace transitive dependents of the symbols declared in the changed files
and filter them to test files (Go: *_test.go). With no file args and stdin piped,
reads ` + "`git diff --name-only`" + ` from stdin; with no args and a TTY, uses the
working-tree changes. The inverse question of ` + "`impact`" + `: given what I
changed, which tests should I run? Read-only.`,
	Args: cobra.ArbitraryArgs,
	RunE: jsonAwareRunE(runGraphAffected),
}

func runGraphAffected(cmd *cobra.Command, args []string) error {
	depth, _ := cmd.Flags().GetInt("depth")
	force, _ := cmd.Flags().GetBool("reindex")
	ctx := cmd.Context()

	if err := rejectOutsideRepoAffectedArgs(ctx, args); err != nil {
		return err
	}

	files, err := resolveAffectedFiles(cmd, args)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no changed files to analyze")
	}

	_, astRepo, client, err := openASTQuery(ctx, "", force, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer client.Close()

	result, err := application.NewAffectedService(astRepo).Affected(ctx, files, depth)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if outputFormat(cmd) == output.FormatJSON {
		return output.EncodeJSON(out, result)
	}
	renderAffected(out, result)
	return nil
}

// rejectOutsideRepoAffectedArgs mirrors impact's rejectOutsideRepoArgs for the
// affected command: an explicit path that resolves outside the repository is a
// hard error (exit 1), not a silently empty result. See impact.go for the rule.
func rejectOutsideRepoAffectedArgs(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return nil
	}
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	return rejectOutsideRepoArgs(root, cwd, args)
}

// resolveAffectedFiles picks the changed-file source: explicit args, else piped
// stdin (one path per line), else the working-tree diff.
func resolveAffectedFiles(cmd *cobra.Command, args []string) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	if stdin := readPipedLines(os.Stdin); len(stdin) > 0 {
		return stdin, nil
	}
	ctx := cmd.Context()
	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo root: %w", err)
	}
	graphGit := infraGit.NewGraphClient(root)
	files, err := graphGit.DiffNameOnly(ctx)
	if err != nil {
		return nil, fmt.Errorf("working-tree diff: %w", err)
	}
	return files, nil
}

func readPipedLines(f *os.File) []string {
	info, err := f.Stat()
	if err != nil || (info.Mode()&os.ModeCharDevice) != 0 {
		return nil
	}
	var lines []string
	sc := bufio.NewScanner(io.LimitReader(f, 1<<20))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func renderAffected(out io.Writer, r *application.AffectedResult) {
	fmt.Fprintf(out, "Affected tests (%d) for %d changed file(s):\n", r.Total, len(r.ChangedFiles))
	if r.Total == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	current := ""
	for _, t := range r.Tests {
		if t.TestFile != current {
			current = t.TestFile
			fmt.Fprintf(out, "  %s\n", t.TestFile)
		}
		fmt.Fprintf(out, "    %s  %s  (line %d, depth %d, via %s)\n", t.Symbol, t.Kind, t.Line, t.Depth, t.Via)
	}
}

func init() {
	graphAffectedCmd.Flags().Int("depth", 2, "transitive caller traversal depth")
	graphAffectedCmd.Flags().Bool("reindex", false, "force a full AST re-index before tracing")
	graphCmd.AddCommand(graphAffectedCmd)
}
