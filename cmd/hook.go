package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fradser/git-agent/domain/hook"
	"github.com/fradser/git-agent/domain/project"
	infraGit "github.com/fradser/git-agent/infrastructure/git"
	infraHook "github.com/fradser/git-agent/infrastructure/hook"
	agentErrors "github.com/fradser/git-agent/pkg/errors"
)

var hookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Internal hook runner (invoked by git hook shims)",
	Hidden: true,
}

var hookRunCmd = &cobra.Command{
	Use:   "run <hook-name> [args...]",
	Short: "Run a git-agent hook by name",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runHook,
}

func runHook(cmd *cobra.Command, args []string) error {
	hookName := args[0]
	switch hookName {
	case "commit-msg":
		if len(args) < 2 {
			return fmt.Errorf("commit-msg hook requires the commit message file path as argument")
		}
		return runCommitMsgHook(cmd, args[1])
	default:
		return fmt.Errorf("unknown hook: %s", hookName)
	}
}

func runCommitMsgHook(cmd *cobra.Command, msgFile string) error {
	msgBytes, err := os.ReadFile(msgFile)
	if err != nil {
		return fmt.Errorf("reading commit message file: %w", err)
	}
	message := string(msgBytes)

	gitClient := infraGit.NewClient()
	root, err := gitClient.RepoRoot(cmd.Context())
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}

	projCfg := loadProjectConfig(filepath.Join(root, ".git-agent", "project.yml"))
	if projCfg == nil {
		projCfg = &project.Config{}
	}

	stagedDiff, err := gitClient.StagedDiff(cmd.Context())
	if err != nil {
		return fmt.Errorf("staged diff: %w", err)
	}

	hookPath := filepath.Join(root, ".git-agent", "hooks", "pre-commit")
	executor := infraHook.NewCompositeHookExecutor()
	result, err := executor.Execute(cmd.Context(), hookPath, hook.HookInput{
		Diff:          stagedDiff.Content,
		CommitMessage: message,
		StagedFiles:   stagedDiff.Files,
		Config:        *projCfg,
	})
	if err != nil {
		return fmt.Errorf("hook execute: %w", err)
	}

	if result.ExitCode != 0 {
		if result.Stderr != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), result.Stderr)
		}
		return agentErrors.NewExitCodeError(result.ExitCode, "")
	}
	return nil
}

func init() {
	hookCmd.AddCommand(hookRunCmd)
	rootCmd.AddCommand(hookCmd)
}
