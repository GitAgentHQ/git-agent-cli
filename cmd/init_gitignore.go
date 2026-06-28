package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGitignore "github.com/gitagenthq/git-agent/infrastructure/gitignore"
	infraGraph "github.com/gitagenthq/git-agent/infrastructure/graph"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
)

func runGitignore(cmd *cobra.Command, out io.Writer) error {
	providerCfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if providerCfg == nil || providerCfg.APIKey == "" {
		return fmt.Errorf("error: no API key configured\nhint: set --api-key flag or add api_key to ~/.config/git-agent/config.yml")
	}

	gitClient := infraGit.NewClient()
	openaiClient := infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model, 0, 0, nil)
	toptalClient := infraGitignore.NewToptalClient()
	svc := application.NewGitignoreService(openaiClient, toptalClient, gitClient)

	techs, err := svc.Generate(cmd.Context(), application.GitignoreRequest{})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, ".gitignore updated: %s\n", strings.Join(techs, ", "))
	return nil
}

// untrackGraphDB removes .git-agent/graph.db from the git index if it is tracked,
// leaving the working-tree file in place. The ignore rule written by runGitignore
// only takes effect once the file is no longer tracked; without this, a previously
// committed graph.db keeps getting re-staged on every run (the "infinite
// recreation" loop). Safe to call when the file is not tracked or does not exist.
func untrackGraphDB(cmd *cobra.Command) error {
	gitClient := infraGit.NewClient()
	ctx := cmd.Context()
	root, err := gitClient.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	untracked, err := ensureGraphDBUntracked(ctx, gitClient, root)
	if err != nil {
		return err
	}
	if untracked {
		fmt.Fprintf(cmd.OutOrStdout(), "Untracked %s (now ignored by .gitignore)\n",
			filepath.Join(root, infraGraph.GraphDBRelPath))
	}
	return nil
}

// ensureGraphDBUntracked removes .git-agent/graph.db from the git index if it is
// tracked, leaving the working-tree file in place. It returns true when it
// actually untracked the file (false on the no-op path). Shared by init (which
// reports the action) and the runtime graph-db open paths (capture/timeline/
// impact), so the "infinite recreation" loop is broken even when init has not
// run — e.g. a repo cloned from a fork that already committed graph.db. Both
// the IsTracked guard and UntrackFile are idempotent, so this is safe to call
// on every graph-db open.
func ensureGraphDBUntracked(ctx context.Context, gitClient *infraGit.Client, root string) (bool, error) {
	rel := infraGraph.GraphDBRelPath
	tracked, err := gitClient.IsTracked(ctx, rel)
	if err != nil {
		return false, fmt.Errorf("check tracked graph.db: %w", err)
	}
	if !tracked {
		return false, nil
	}
	if err := gitClient.UntrackFile(ctx, rel); err != nil {
		return false, fmt.Errorf("untrack %s: %w", rel, err)
	}
	return true, nil
}
