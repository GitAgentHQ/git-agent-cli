package cmd

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

var verbose bool
var freeMode bool

var rootCmd = &cobra.Command{
	Use:          "git-agent",
	Short:        "AI-first Git CLI for automated commit message generation",
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		var exitErr *agentErrors.ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}

func ExecuteArgs(args []string) error {
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func userConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git-agent", "config.yml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git-agent", "config.yml")
}

func resolveProviderConfig(cmd *cobra.Command) (*infraConfig.ProviderConfig, error) {
	apiKey, _ := cmd.Flags().GetString("api-key")
	model, _ := cmd.Flags().GetString("model")
	baseURL, _ := cmd.Flags().GetString("base-url")
	return infraConfig.Resolve(cmd.Context(), infraConfig.ProviderConfig{
		APIKey:   apiKey,
		Model:    model,
		BaseURL:  baseURL,
		FreeMode: freeMode,
	}, userConfigPath())
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&freeMode, "free", false, "use only build-time embedded credentials; ignore config file and git config")
	rootCmd.PersistentFlags().String("api-key", "", "API key for the AI provider")
	rootCmd.PersistentFlags().String("model", "", "model to use for generation")
	rootCmd.PersistentFlags().String("base-url", "", "base URL for the AI provider")
	rootCmd.MarkFlagsMutuallyExclusive("free", "api-key")
	rootCmd.MarkFlagsMutuallyExclusive("free", "model")
	rootCmd.MarkFlagsMutuallyExclusive("free", "base-url")
}
