package typesafe

import (
	"context"
	"fmt"
	"strings"

	"github.com/gitagenthq/git-agent/domain/project"
)

// Option names that carry a decision instead of a scope name.
const (
	optionNew  = "new"
	optionSkip = "skip"
)

// maxCandidateDirs caps how many directories one request judges. Jev prices
// input tokens and the fan-out is already one round trip, so the cap only
// guards a repository whose root holds an unreasonable number of directories.
const maxCandidateDirs = 64

// ScopeDecider asks Jev which scope covers each top-level directory.
//
// Code owns the candidate set: it filters the repository directories and derives
// every legal scope name. Jev only selects among those candidates, so it decides
// which scope covers a directory, whether the directory needs a new scope, or
// whether it needs no scope at all. The model that writes scope descriptions
// runs afterwards and cannot change this outcome.
type ScopeDecider struct {
	client *Client
}

// NewScopeDecider wires a decider to a client.
func NewScopeDecider(client *Client) *ScopeDecider {
	return &ScopeDecider{client: client}
}

const scopeStateHeader = "Existing scopes, their descriptions, the tracked files, and the recent commit log follow. Judge only the top-level directories."

// DecideScopes evaluates every candidate directory in one request and returns
// one assignment per directory.
func (d *ScopeDecider) DecideScopes(ctx context.Context, req project.ScopeDecisionRequest) (*project.ScopeDecision, error) {
	if d.client == nil {
		return nil, fmt.Errorf("typesafe: no client configured")
	}

	dirs := project.CandidateDirs(req.Dirs)
	if len(dirs) > maxCandidateDirs {
		dirs = dirs[:maxCandidateDirs]
	}
	if len(dirs) == 0 {
		return &project.ScopeDecision{}, nil
	}

	options := scopeOptions(req.ExistingScopes)
	questions := make(map[string]Question, len(dirs))
	order := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		id := "dir_" + dir
		questions[id] = Choice(map[string]any{
			"directory": dir,
			"judgement": "Which scope covers this directory? Answer \"" + optionNew +
				"\" when the directory needs its own new scope, and \"" + optionSkip +
				"\" when the directory needs no scope because it holds build output, " +
				"dependencies, tooling, or documentation.",
		}, options)
		order = append(order, dir)
	}

	result, err := d.client.Evaluate(ctx, scopeState(req, dirs), questions)
	if err != nil {
		return nil, err
	}

	decision := &project.ScopeDecision{Confidence: 1}
	for _, dir := range order {
		answer, ok := result.Answer("dir_" + dir)
		if !ok || answer.Choice == nil {
			// A missing answer means this directory stays undecided, which
			// code reads as "needs a new scope" only when the model said so.
			// Treating a missing answer as skip keeps an undecided
			// directory from adding a scope nobody asked for.
			continue
		}
		if answer.Confidence != nil && *answer.Confidence < decision.Confidence {
			decision.Confidence = *answer.Confidence
		}
		chosen := *answer.Choice
		switch {
		case chosen == optionSkip:
			decision.Assignments = append(decision.Assignments,
				project.ScopeAssignment{Dir: dir, Action: project.ScopeSkip})
		case chosen == optionNew:
			decision.Assignments = append(decision.Assignments,
				project.ScopeAssignment{Dir: dir, Action: project.ScopeCreate, Scope: project.DeriveScopeName(dir)})
		case scopeExists(req.ExistingScopes, chosen):
			decision.Assignments = append(decision.Assignments,
				project.ScopeAssignment{Dir: dir, Action: project.ScopeReuse, Scope: chosen})
		}
	}
	return decision, nil
}

// scopeState assembles the evidence for the judgement. The commit log comes
// last and stays capped, because it carries the same signal as the file list at
// a much higher token cost.
func scopeState(req project.ScopeDecisionRequest, dirs []string) map[string]any {
	state := map[string]any{
		"purpose":        scopeStateHeader,
		"directories":    dirs,
		"existingScopes": req.ExistingScopes,
	}
	if len(req.Files) > 0 {
		state["files"] = req.Files
	}
	if len(req.Commits) > 0 {
		state["recentCommits"] = capItems(req.Commits, maxStateCommits)
	}
	return state
}

// maxStateCommits bounds the commit log in the state so one long history cannot
// crowd out the file list.
const maxStateCommits = 60

func capItems(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	return items[:max]
}

// scopeOptions lists the existing scopes with their descriptions, plus the two
// decision options. An option is missing from the map when it was not offered,
// which is how an answer outside the candidate set is rejected.
func scopeOptions(existing []project.Scope) map[string]string {
	options := make(map[string]string, len(existing)+2)
	for _, s := range existing {
		if s.Name == "" {
			continue
		}
		if s.Description == "" {
			options[s.Name] = "An existing scope with no description."
			continue
		}
		options[s.Name] = s.Description
	}
	options[optionNew] = "The directory needs its own new scope."
	options[optionSkip] = "The directory needs no scope: build output, dependencies, tooling, or documentation."
	return options
}

func scopeExists(existing []project.Scope, name string) bool {
	for _, s := range existing {
		if strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}
