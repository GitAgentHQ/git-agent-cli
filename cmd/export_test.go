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

// CommitServiceHeuristicPlannerForTest returns the service's fallback planner
// (or nil when fallback is disabled).
func CommitServiceHeuristicPlannerForTest(s *application.CommitService) any {
	if p := s.HeuristicPlanner(); p != nil {
		return p
	}
	return nil
}
