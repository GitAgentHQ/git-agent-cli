package commit

import (
	"context"
	"strings"
)

// GroupDecider decides how changed files split into atomic commits.
//
// Code owns the candidate set: it buckets the files by a deterministic rule and
// the decider only says which buckets belong together and which must be split
// apart. A decider returns groups of buckets, never a group of invented files,
// so a hallucinated path cannot reach the staging step.
type GroupDecider interface {
	DecideGroups(ctx context.Context, req GroupDecisionRequest) (*GroupDecision, error)
}

// GroupDecisionRequest carries the candidate buckets and the evidence a
// decider needs to judge them. Buckets hold file paths; every bucket describes
// one candidate commit.
type GroupDecisionRequest struct {
	Buckets []FileBucket `json:"buckets"`
	// Scopes maps a scope name to its description, so a decider can label a
	// bucket by its subject instead of by its paths.
	Scopes map[string]string `json:"scopes"`
}

// FileBucket is one candidate commit: a deterministic group of changed files.
type FileBucket struct {
	// ID is the bucket's stable name inside one decision, such as "a", "b".
	ID string `json:"id"`
	// Label is a short human-readable name, normally the shared directory or
	// the file stem.
	Label string   `json:"label"`
	Files []string `json:"files"`
	Adds  int      `json:"adds"`
	Dels  int      `json:"dels"`
	// Symbols holds the function or type names the diff touched, capped by the
	// caller. It is the strongest cheap signal for what a bucket is about.
	Symbols []string `json:"symbols,omitempty"`
}

// GroupDecision maps every candidate bucket onto exactly one output group.
type GroupDecision struct {
	// GroupBuckets lists bucket ids per commit group, in commit order. Every
	// bucket in the request must appear exactly once.
	GroupBuckets [][]string `json:"groupBuckets"`
	// Confidence is how strongly the decider committed to its own answer: the
	// mean probability of the option each bucket chose. It is reported by the
	// model, not derived from the shape of the grouping, because a shape count
	// reads as a probability and is not one.
	Confidence float64 `json:"confidence"`
}

// DecidedGroups resolves a decision against the request buckets and returns the
// file list per group. A bucket the decision left out, or named twice, is
// reported through problems instead of being guessed at.
func (d *GroupDecision) DecidedGroups(req GroupDecisionRequest) ([][]string, []string) {
	if d == nil {
		return nil, []string{"decision is empty"}
	}
	byID := make(map[string]FileBucket, len(req.Buckets))
	for _, b := range req.Buckets {
		byID[b.ID] = b
	}

	var groups [][]string
	var problems []string
	seen := make(map[string]bool, len(req.Buckets))
	for _, bucketIDs := range d.GroupBuckets {
		var files []string
		for _, id := range bucketIDs {
			bucket, ok := byID[id]
			if !ok {
				problems = append(problems, "unknown bucket "+id)
				continue
			}
			if seen[id] {
				problems = append(problems, "duplicate bucket "+id)
				continue
			}
			seen[id] = true
			files = append(files, bucket.Files...)
		}
		if len(files) > 0 {
			groups = append(groups, files)
		}
	}
	for _, b := range req.Buckets {
		if !seen[b.ID] {
			problems = append(problems, "bucket left undecided "+b.ID)
		}
	}
	return groups, problems
}

// FailureRouter decides how to answer a rejected commit.
//
// The commit hook already states why it rejected a message. Routing that
// reason is a decision: a wording problem needs a rewrite, a grouping problem
// needs a new plan, and an intent mismatch needs neither. Code fixes only the
// lever the router names.
type FailureRouter interface {
	RouteFailure(ctx context.Context, req FailureRouteRequest) (*FailureRoute, error)
}

// FailureAction is the lever a rejection needs.
type FailureAction string

const (
	// FailureRewrite asks for a new message for the same file group.
	FailureRewrite FailureAction = "rewrite"
	// FailureReplan asks for a new grouping of the remaining files.
	FailureReplan FailureAction = "replan"
	// FailureGiveUp ends the commit with the hook's reason.
	FailureGiveUp FailureAction = "give_up"
)

// FailureRouteRequest describes the rejection and both levers that could fix it.
type FailureRouteRequest struct {
	// HookReason is the verbatim reason the hook reported.
	HookReason string `json:"hookReason"`
	// Title and Bullets are the rejected message.
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
	// Files are the files of the rejected group.
	Files []string `json:"files"`
	// RemainingFiles are the files not yet committed.
	RemainingFiles []string `json:"remainingFiles"`
}

// FailureRoute is the router's verdict. Confidence is reported so the caller
// can fall back to the fixed retry policy when the verdict is weak.
type FailureRoute struct {
	Action     FailureAction `json:"action"`
	Confidence float64       `json:"confidence"`
	Reason     string        `json:"reason"`
}

// TypeScopeJudge decides the conventional-commit type and scope of one change.
//
// The judge owns the prefix and the generative provider owns everything after
// it. It runs only where the evidence supports it: the state is a structured
// summary of the change, never the diff body, and the judge is skipped for a
// language whose accuracy is unmeasured.
type TypeScopeJudge interface {
	JudgeTypeScope(ctx context.Context, req TypeScopeRequest) (*TypeScopeVerdict, error)
}

// TypeScopeRequest carries the structured summary of one change.
type TypeScopeRequest struct {
	Files   []FileChange `json:"files"`
	Symbols []string     `json:"symbols,omitempty"`
	Intent  string       `json:"intent,omitempty"`
	// Scopes maps every configured scope name to its description.
	Scopes map[string]string `json:"scopes"`
	// Language is the language the commit message will use. A judge receives
	// it so it can refuse a language it has not been measured on.
	Language string `json:"language,omitempty"`
}

// FileChange is one changed file with its line counts.
type FileChange struct {
	Path string `json:"path"`
	Adds int    `json:"adds"`
	Dels int    `json:"dels"`
}

// TypeScopeVerdict is the judge's answer.
//
// The two parts are reported separately because they are not equally reliable:
// measurement over a repository's own history put scope agreement far above type
// agreement. A caller gates each part on its own floor, so the reliable part can
// act while the unreliable one stays with the model.
type TypeScopeVerdict struct {
	Type  string `json:"type"`
	Scope string `json:"scope"`
	// TypeConfidence is how strongly the judge committed to the type.
	TypeConfidence float64 `json:"typeConfidence"`
	// ScopeConfidence is how strongly it committed to the scope.
	ScopeConfidence float64 `json:"scopeConfidence"`
	// Probabilities maps each offered option to its probability, prefixed with
	// "type:" or "scope:", so a caller can apply its own rule over the
	// distribution.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Confidence returns the weaker of the two confidences, for a caller that treats
// the verdict as one decision.
func (v TypeScopeVerdict) Confidence() float64 {
	if v.ScopeConfidence < v.TypeConfidence {
		return v.ScopeConfidence
	}
	return v.TypeConfidence
}

// PinnedScope reports the scope a caller may pin, empty when the scope did not
// clear its floor.
func (v TypeScopeVerdict) PinnedScope(scopeAllowed bool) string {
	if !scopeAllowed {
		return ""
	}
	return v.Scope
}

// JudgeSupportsLanguage reports whether a judgment may decide for a commit
// message written in this language.
//
// TypeSafe measures its best accuracy in English and states that other languages
// need their own measurement, and the CLI has no such measurement for any language
// yet. So English decides and every other language keeps its provider. The value
// is a language tag such as "zh-CN"; an empty value means the CLI chose no
// language, which is English by default.
func JudgeSupportsLanguage(language string) bool {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if normalized == "" {
		return true
	}
	primary, _, _ := strings.Cut(normalized, "-")
	switch primary {
	case "en", "english":
		return true
	default:
		return false
	}
}

// ConventionalTypes are the commit types this project accepts in a title prefix.
var ConventionalTypes = []string{
	"feat", "fix", "docs", "refactor", "test", "chore", "perf", "style", "build", "ci", "revert",
}
