package project

import "context"

// ScopeAction is what a decider decided for one candidate directory.
type ScopeAction string

const (
	// ScopeReuse means the directory belongs to a scope that already exists.
	ScopeReuse ScopeAction = "reuse"
	// ScopeCreate means the directory needs its own new scope.
	ScopeCreate ScopeAction = "create"
	// ScopeSkip means the directory needs no scope (build output, dependencies,
	// tooling, or documentation).
	ScopeSkip ScopeAction = "skip"
)

// ScopeAssignment binds one candidate directory to a decider verdict.
type ScopeAssignment struct {
	Dir    string      `json:"dir"`
	Action ScopeAction `json:"action"`
	// Scope is the target scope name. It holds the reused scope name for
	// ScopeReuse and the derived new scope name for ScopeCreate. It is empty
	// for ScopeSkip.
	Scope string `json:"scope,omitempty"`
}

// ScopeDecisionRequest carries the evidence a decider judges. Dirs holds the
// top-level directories, Files the tracked files, and Commits the recent commit
// log entries.
type ScopeDecisionRequest struct {
	Dirs           []string `json:"dirs"`
	Files          []string `json:"files"`
	Commits        []string `json:"commits"`
	ExistingScopes []Scope  `json:"existingScopes"`
}

// ScopeDecision is the decider's verdict, one assignment per candidate
// directory it judged.
type ScopeDecision struct {
	Assignments []ScopeAssignment `json:"assignments"`
	// Confidence is the weakest confidence across the assignments. Scope
	// assignment answered at or above 0.72 in measurement, so a caller with a
	// floor below that can act on the whole verdict as one decision.
	Confidence float64 `json:"confidence"`
}

// ScopeDecider decides which scope covers each top-level directory.
//
// The decider owns the decision. The model owns the wording of a scope
// description. A nil decider disables the layer, and the caller falls back to
// asking the model for the scope set directly.
type ScopeDecider interface {
	DecideScopes(ctx context.Context, req ScopeDecisionRequest) (*ScopeDecision, error)
}

// ProposedScope is a new scope a decision asks the model to describe.
type ProposedScope struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// Created returns the new scopes the decision asks for, in directory order.
// A create assignment without a scope name is skipped, because a scope with no
// name cannot be written to the project config.
func (d *ScopeDecision) Created() []ProposedScope {
	if d == nil {
		return nil
	}
	var out []ProposedScope
	for _, a := range d.Assignments {
		if a.Action != ScopeCreate || a.Scope == "" {
			continue
		}
		out = append(out, ProposedScope{Name: a.Scope, Dir: a.Dir})
	}
	return out
}
