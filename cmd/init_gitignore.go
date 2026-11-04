package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/application"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraGitignore "github.com/gitagenthq/git-agent/infrastructure/gitignore"
)

func runGitignore(cmd *cobra.Command) error {
	providerCfg, err := resolveProviderConfig(cmd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if configErr := providerConfigError(providerCfg); configErr != "" {
		return fmt.Errorf("%s", configErr)
	}

	gitClient := infraGit.NewClient()
	openaiClient := newLLMClient(providerCfg, nil)
	toptalClient := infraGitignore.NewToptalClient()
	svc := application.NewGitignoreService(openaiClient, toptalClient, gitClient)

	techs, _, err := svc.Generate(cmd.Context(), application.GitignoreRequest{})
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), ".gitignore updated: %s\n", strings.Join(techs, ", "))
	return nil
}
