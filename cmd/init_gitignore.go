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
	if configErr := providerConfigError(providerCfg); configErr != "" {
		return fmt.Errorf("%s", configErr)
	}

	gitClient := infraGit.NewClient()
	openaiClient := infraOpenAI.NewClient(
		providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model,
		providerCfg.RequestTimeout, providerCfg.HeartbeatInterval, nil,
	)
	openaiClient.SetCloudflareAIGateway(providerCfg.CloudflareAIGatewayID)
	toptalClient := infraGitignore.NewToptalClient()
	svc := application.NewGitignoreService(openaiClient, toptalClient, gitClient)

	techs, _, err := svc.Generate(cmd.Context(), application.GitignoreRequest{})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, ".gitignore updated: %s\n", strings.Join(techs, ", "))
	return nil
}
