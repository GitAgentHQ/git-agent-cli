package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGitignore "github.com/gitagenthq/git-agent/infrastructure/gitignore"
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
	openaiClient := infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model)
	toptalClient := infraGitignore.NewToptalClient()
	svc := application.NewGitignoreService(openaiClient, toptalClient, gitClient)

	techs, err := svc.Generate(cmd.Context(), application.GitignoreRequest{})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, ".gitignore updated: %s\n", strings.Join(techs, ", "))
	return nil
}
