package cmd

import (
	"io"

	domainCommit "github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/decision"
	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
	"github.com/gitagenthq/git-agent/domain/project"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
	infraTypesafe "github.com/gitagenthq/git-agent/infrastructure/typesafe"

	"github.com/gitagenthq/git-agent/application"
)

// jevLayer is the optional System One layer for one command run.
//
// Every seam it carries is optional, and the whole layer is nil when no
// TypeSafe key is configured or the mode is off. A nil layer is the documented
// off switch: every decision goes to the generative provider, which is how
// git-agent behaved before the layer existed.
type jevLayer struct {
	decisions *application.Decisions
	client    *infraTypesafe.Client
}

// newJevLayer builds the layer from resolved config. A missing key or the off
// mode returns nil, so no caller has to check the key itself.
func newJevLayer(cfg *infraConfig.ProviderConfig, log io.Writer) *jevLayer {
	if cfg == nil || cfg.JevAPIKey == "" {
		return nil
	}
	mode := decision.ParseMode(cfg.JevMode)
	if mode == decision.ModeOff {
		return nil
	}
	client := infraTypesafe.NewClient(cfg.JevAPIKey, cfg.JevBaseURL, cfg.JevModel, cfg.RequestTimeout)
	return &jevLayer{
		decisions: application.NewDecisions(mode, cfg.JevMinConfidence, cfg.JevMaxCalls, nil, log),
		client:    client,
	}
}

// Decisions returns the layer's policy, or nil when the layer is off. Reading
// the field directly would dereference a nil layer, so every caller goes
// through this method.
func (l *jevLayer) Decisions() *application.Decisions {
	if l == nil {
		return nil
	}
	return l.decisions
}

// ScopeDecider returns the scope decision seam, or nil when the layer is off.
func (l *jevLayer) ScopeDecider() project.ScopeDecider {
	if l == nil {
		return nil
	}
	return infraTypesafe.NewScopeDecider(l.client)
}

// TechClassifier returns the technology classification seam.
func (l *jevLayer) TechClassifier() domainGitignore.TechnologyClassifier {
	if l == nil {
		return nil
	}
	return infraTypesafe.NewTechClassifier(l.client)
}

// GroupDecider returns the commit grouping seam.
func (l *jevLayer) GroupDecider() domainCommit.GroupDecider {
	if l == nil {
		return nil
	}
	return infraTypesafe.NewGroupDecider(l.client)
}

// TypeScopeJudge returns the commit prefix seam.
func (l *jevLayer) TypeScopeJudge() domainCommit.TypeScopeJudge {
	if l == nil {
		return nil
	}
	return infraTypesafe.NewTypeScopeJudge(l.client)
}

// FailureRouter returns the hook rejection routing seam.
func (l *jevLayer) FailureRouter() domainCommit.FailureRouter {
	if l == nil {
		return nil
	}
	return infraTypesafe.NewFailureRouter(l.client)
}

// Apply attaches the layer to the commit service and its scope service.
func (l *jevLayer) Apply(svc *application.CommitService, scopeSvc *application.ScopeService) {
	if l == nil || svc == nil {
		return
	}
	svc.WithJudgments(l.GroupDecider(), l.TypeScopeJudge(), l.FailureRouter()).WithDecisions(l.decisions)
	if scopeSvc != nil {
		scopeSvc.WithDecisions(l.decisions)
	}
}
