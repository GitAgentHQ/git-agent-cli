package cmd

import (
	"io"
	"time"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/project"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraGit "github.com/gitagenthq/git-agent/infrastructure/git"
	infraOpenAI "github.com/gitagenthq/git-agent/infrastructure/openai"
)

// BuildCommitDepsForTest exposes buildCommitDeps so the cmd-layer wiring
// (request_timeout / heartbeat_interval / plan_fallback → constructors) can
// be inspected without running a full commit. The git client is a real
// infraGit.Client because none of the inspected fields touch git.
func BuildCommitDepsForTest(providerCfg *infraConfig.ProviderConfig, projCfg *project.Config) (*infraOpenAI.Client, *application.CommitService) {
	return buildCommitDeps(providerCfg, projCfg, infraGit.NewClient(), io.Discard)
}

// OpenAIRequestTimeoutForTest is a thin shim over the openai client's
// RequestTimeout accessor — kept in cmd so the test only imports cmd helpers.
func OpenAIRequestTimeoutForTest(c *infraOpenAI.Client) time.Duration {
	return c.RequestTimeout()
}

// OpenAIHeartbeatIntervalForTest mirrors OpenAIRequestTimeoutForTest for the
// heartbeat interval.
func OpenAIHeartbeatIntervalForTest(c *infraOpenAI.Client) time.Duration {
	return c.HeartbeatInterval()
}

func HasUncoveredDirsForTest(allFiles []string, scopes []project.Scope) bool {
	return hasUncoveredDirs(allFiles, scopes)
}

// OpenAICloudflareAIGatewayIDForTest reports the gateway ID wired from user config.
func OpenAICloudflareAIGatewayIDForTest(c *infraOpenAI.Client) string {
	return c.CloudflareAIGatewayID()
}

// OpenAIMaxInputTokensForTest mirrors OpenAIRequestTimeoutForTest for the
// preflight input-size ceiling threaded from max_input_tokens.
func OpenAIMaxInputTokensForTest(c *infraOpenAI.Client) int {
	return c.MaxInputTokens()
}

// ProviderConfigErrorForTest exposes providerConfigError so the free-gateway
// / direct / no-provider validation modes can be unit-tested in isolation.
func ProviderConfigErrorForTest(cfg *infraConfig.ProviderConfig) string {
	return providerConfigError(cfg)
}

// ResetRootFlags resets root command flags to their default empty state.
func ResetRootFlags() {
	_ = rootCmd.Flags().Set("api-key", "")
	_ = rootCmd.Flags().Set("base-url", "")
	_ = rootCmd.Flags().Set("model", "")
	_ = rootCmd.Flags().Set("free", "false")
	_ = rootCmd.Flags().Set("verbose", "false")
	_ = rootCmd.PersistentFlags().Set("api-key", "")
	_ = rootCmd.PersistentFlags().Set("base-url", "")
	_ = rootCmd.PersistentFlags().Set("model", "")
	_ = rootCmd.PersistentFlags().Set("free", "false")
	_ = rootCmd.PersistentFlags().Set("verbose", "false")
}

// CommitServiceHeuristicPlannerForTest returns the service's fallback planner
// (or nil when fallback is disabled).
func CommitServiceHeuristicPlannerForTest(s *application.CommitService) any {
	if p := s.HeuristicPlanner(); p != nil {
		return p
	}
	return nil
}
