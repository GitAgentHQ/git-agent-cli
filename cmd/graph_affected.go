package cmd

import (
	"bufio"
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
	RunE: runGraphAffected,
}

func runGraphAffected(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	textFlag, _ := cmd.Flags().GetBool("text")
	depth, _ := cmd.Flags().GetInt("depth")
	force, _ := cmd.Flags().GetBool("reindex")
	ctx := cmd.Context()

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
	if output.Decide(jsonFlag, textFlag) == output.FormatJSON {
		return output.EncodeJSON(out, result)
	}
	renderAffected(out, result)
	return nil
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
	graphAffectedCmd.Flags().Bool("json", false, "emit the affected set as JSON")
	graphAffectedCmd.Flags().Bool("text", false, "emit the affected set as text")
	graphAffectedCmd.MarkFlagsMutuallyExclusive("json", "text")
	graphCmd.AddCommand(graphAffectedCmd)
}
