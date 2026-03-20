package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/fradser/git-agent/application"
	infraConfig "github.com/fradser/git-agent/infrastructure/config"
	infraGit "github.com/fradser/git-agent/infrastructure/git"
	infraGitignore "github.com/fradser/git-agent/infrastructure/gitignore"
	infraOpenAI "github.com/fradser/git-agent/infrastructure/openai"
)

func runGitignore(ctx context.Context, force bool, out io.Writer) error {
	providerCfg, err := infraConfig.Resolve(ctx, infraConfig.ProviderConfig{}, userConfigPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if providerCfg == nil || providerCfg.APIKey == "" {
		return fmt.Errorf("error: no API key configured\nhint: set --api-key flag or add api_key to ~/.config/git-agent/config.yml")
	}

	gitClient := infraGit.NewClient()
	if !gitClient.IsGitRepo(ctx) {
		return fmt.Errorf("not a git repository")
	}

	openaiClient := infraOpenAI.NewClient(providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Model)
	toptalClient := infraGitignore.NewToptalClient()
	svc := application.NewGitignoreService(openaiClient, toptalClient, gitClient)

	techs, err := svc.Generate(ctx, application.GitignoreRequest{Force: force})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "generated .gitignore for: %s\n", strings.Join(techs, ", "))
	return nil
}
