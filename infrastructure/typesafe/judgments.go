package typesafe

import (
	"context"
	"fmt"
	"sort"
	"strings"

	domainCommit "github.com/gitagenthq/git-agent/domain/commit"
	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
	"github.com/gitagenthq/git-agent/domain/project"
)

// maxBucketsPerRequest bounds how many candidate buckets one request judges.
// A request over this count is split across calls.
const maxBucketsPerRequest = 24

// maxSymbolItems caps the symbol list per bucket. The symbols are the strongest
// cheap signal for what a bucket is about, and the cap bounds the state cost.
const maxSymbolItems = 8

// fileChangesCap caps the file list in a judge state.
const fileChangesCap = 120

// TechClassifier decides which technologies a project uses by asking one
// presence question per candidate identifier. All identifiers travel in one
// request, so the whole classification is one round trip.
type TechClassifier struct {
	client *Client
}

// NewTechClassifier wires a classifier to a client.
func NewTechClassifier(client *Client) *TechClassifier {
	return &TechClassifier{client: client}
}

// TechFloor reports a floor no TechClassifier can reach.
//
// Measured against the technology list each repository's generated .gitignore
// already records, the classifier was exact on a Go repository and identified
// one of four technologies on a React repository, missing react and node. A
// .gitignore without those rules is broken output, so the seam no longer
// decides. It still runs in shadow mode so the comparison keeps accumulating.
func (c *TechClassifier) TechFloor() float64 { return unreachableFloor }

// ClassifyTechnologies returns the candidate identifiers the state supports,
// each above presenceThreshold. The OS identifier is added when the state names
// an operating system, because a project always ignores files for its own
// platform.
func (c *TechClassifier) ClassifyTechnologies(ctx context.Context, req domainGitignore.ClassifyRequest) (*domainGitignore.TechnologyVerdict, error) {
	if c.client == nil {
		return nil, fmt.Errorf("typesafe: no client configured")
	}

	dirs := project.CandidateDirs(req.Dirs)
	files := req.Files
	if len(files) > fileChangesCap {
		files = files[:fileChangesCap]
	}
	state := map[string]any{
		"purpose":      "Decide which of the offered technologies this project actually uses. Judge only from the evidence in this state.",
		"os":           req.OS,
		"directories":  dirs,
		"files":        files,
		"fileCount":    len(req.Files),
		"shownFileCap": fileChangesCap,
	}

	questions := make(map[string]Question, len(domainGitignore.CandidateTechnologies))
	for _, candidate := range domainGitignore.CandidateTechnologies {
		questions["tech_"+candidate.ID] = Noul(
			map[string]any{
				"technology": candidate.ID,
				"judgement":  "Does this project use " + candidate.ID + "?",
			},
			&NoulCriteria{Yes: candidate.Evidence, No: "The state holds no evidence of it."},
		)
	}

	result, err := c.client.Evaluate(ctx, state, questions)
	if err != nil {
		return nil, err
	}

	var present []string
	weakest := 1.0
	for _, candidate := range domainGitignore.CandidateTechnologies {
		answer, ok := result.Answer("tech_" + candidate.ID)
		if !ok || answer.Noul == nil {
			continue
		}
		if *answer.Noul < presenceThreshold {
			continue
		}
		if *answer.Noul < weakest {
			weakest = *answer.Noul
		}
		present = append(present, candidate.ID)
	}

	if os := osIdentifier(req.OS); os != "" && !contains(present, os) {
		// The platform is decided by code, not by the model, so it does not
		// lower the verdict's confidence.
		present = append([]string{os}, present...)
	}
	if len(present) == 0 {
		return nil, fmt.Errorf("jev found no technology in %d candidate(s)", len(domainGitignore.CandidateTechnologies))
	}
	return &domainGitignore.TechnologyVerdict{Technologies: present, Confidence: weakest}, nil
}

// presenceThreshold is the probability at which a presence answer counts. A
// closed identifier set makes an omission harmless and a wrong inclusion
// expensive, because the identifier adds ignore rules for files the project does
// not have.
const presenceThreshold = 0.5

// osIdentifier maps a runtime OS name to its Toptal identifier.
func osIdentifier(os string) string {
	switch strings.ToLower(strings.TrimSpace(os)) {
	case "darwin", "macos":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return ""
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// GroupDecider decides which candidate buckets belong in the same commit.
//
// Code buckets the files and the decider only merges and splits buckets, so a
// group can never name a file the repository did not change. The evidence is a
// structured summary per bucket: its label, line counts, and touched symbols.
// The diff body never travels to this judgment.
type GroupDecider struct {
	client *Client
}

// NewGroupDecider wires a decider to a client.
func NewGroupDecider(client *Client) *GroupDecider {
	return &GroupDecider{client: client}
}

// GroupFloor reports a floor no GroupDecider can reach.
//
// Measured against a real generative provider over three repositories, this
// judgment lost every hand-adjudicated case by over-splitting one logical change
// into one commit per top-level directory. The cause is in the candidate set
// rather than in the model: it is the top-level directory, and the evidence is
// paths and line counts with no diff, so a change spanning five directories as
// one feature reads as five commits.
//
// The seam therefore no longer decides. It still runs in shadow mode, so the
// comparison keeps accumulating, and its answers stay visible in verbose output.
// Raising this value is how the seam would be promoted again, and only a
// measurement against the baseline can justify it.
func (j *GroupDecider) GroupFloor() float64 { return unreachableFloor }

// unreachableFloor is above every probability the API can return, so a seam
// that publishes it is measured and never adopted.
const unreachableFloor = 1.1

// DecideGroups returns the bucket ids per commit group.
func (d *GroupDecider) DecideGroups(ctx context.Context, req domainCommit.GroupDecisionRequest) (*domainCommit.GroupDecision, error) {
	if d.client == nil {
		return nil, fmt.Errorf("typesafe: no client configured")
	}
	buckets := req.Buckets
	if len(buckets) < 2 {
		// One candidate bucket needs no judgment.
		return singleGroupDecision(buckets), nil
	}
	if len(buckets) > maxBucketsPerRequest {
		return nil, fmt.Errorf("jev group decision received %d buckets, above the %d limit", len(buckets), maxBucketsPerRequest)
	}

	questions := make(map[string]Question, len(buckets))
	ids := make([]string, 0, len(buckets))
	for _, b := range buckets {
		ids = append(ids, b.ID)
		questions["bucket_"+b.ID] = Choice(
			map[string]any{
				"bucket": bucketSummary(b),
				"judgement": "Does this change belong in its own commit, together with another bucket, " +
					"or with several buckets? Prefer one bucket per commit unless the buckets clearly " +
					"implement one feature together.",
			},
			mergeOptions(ids, b.ID),
		)
	}

	state := map[string]any{
		"purpose":     "Group the candidate buckets into atomic commits. Judge only from the bucket summaries.",
		"buckets":     bucketSummaries(buckets),
		"bucketCount": len(buckets),
	}
	if len(req.Scopes) > 0 {
		state["scopes"] = req.Scopes
	}

	result, err := d.client.Evaluate(ctx, state, questions)
	if err != nil {
		return nil, err
	}

	// Merge votes: a bucket that names another bucket in the same group pulls
	// both into one group. Two directions count as the same group.
	parent := make(map[string]string, len(buckets))
	var find func(string) string
	find = func(x string) string {
		if parent[x] == "" || parent[x] == x {
			return x
		}
		parent[x] = find(parent[x])
		return parent[x]
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for _, id := range ids {
		parent[id] = id
	}

	for _, id := range ids {
		answer, ok := result.Answer("bucket_" + id)
		if !ok || answer.Choice == nil {
			continue
		}
		for _, other := range strings.Split(*answer.Choice, ",") {
			other = strings.TrimSpace(other)
			if other != "" && other != id {
				union(id, other)
			}
		}
	}

	// Confidence is the mean probability of the option each bucket chose, so the
	// caller gates on what the model committed to rather than on how the answer
	// happens to be shaped.
	var committed float64
	for _, id := range ids {
		answer, ok := result.Answer("bucket_" + id)
		if !ok || answer.Choice == nil {
			continue
		}
		committed += answer.Probability(*answer.Choice)
	}

	order := make(map[string]int, len(ids))
	for i, id := range ids {
		order[id] = i
	}
	groupsByRoot := make(map[string][]string, len(ids))
	var roots []string
	for _, id := range ids {
		root := find(id)
		if _, seen := groupsByRoot[root]; !seen {
			roots = append(roots, root)
		}
		groupsByRoot[root] = append(groupsByRoot[root], id)
	}
	sort.SliceStable(roots, func(i, j int) bool { return order[roots[i]] < order[roots[j]] })

	decision := &domainCommit.GroupDecision{}
	for _, root := range roots {
		decision.GroupBuckets = append(decision.GroupBuckets, groupsByRoot[root])
	}
	if len(ids) > 0 {
		decision.Confidence = committed / float64(len(ids))
	}
	return decision, nil
}

// mergeOptions offers the bucket itself, every single other bucket, and every
// combination of the other buckets as one option. Code holds the option set, so
// the answer is always a grouping code can execute.
func mergeOptions(ids []string, self string) map[string]string {
	options := map[string]string{self: "This change is its own commit."}
	for _, other := range ids {
		if other == self {
			continue
		}
		options[self+","+other] = "This change and one other belong in one commit."
	}
	if len(ids) > 2 {
		options[strings.Join(without(ids, self), ",")] = "These changes implement one feature together."
	}
	return options
}

func without(ids []string, drop string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

func singleGroupDecision(buckets []domainCommit.FileBucket) *domainCommit.GroupDecision {
	if len(buckets) == 0 {
		return &domainCommit.GroupDecision{}
	}
	return &domainCommit.GroupDecision{GroupBuckets: [][]string{{buckets[0].ID}}}
}

// bucketSummary renders one bucket for the structured instructions.
func bucketSummary(b domainCommit.FileBucket) map[string]any {
	summary := map[string]any{
		"id":    b.ID,
		"label": b.Label,
		"files": capStrings(b.Files, 12),
		"adds":  b.Adds,
		"dels":  b.Dels,
	}
	if len(b.Symbols) > 0 {
		summary["touchedSymbols"] = capStrings(b.Symbols, maxSymbolItems)
	}
	return summary
}

func bucketSummaries(buckets []domainCommit.FileBucket) []map[string]any {
	out := make([]map[string]any, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, bucketSummary(b))
	}
	return out
}

func capStrings(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	return values[:max]
}

// FailureRouter decides which lever a rejected commit needs.
type FailureRouter struct {
	client *Client
}

// NewFailureRouter wires a router to a client.
func NewFailureRouter(client *Client) *FailureRouter {
	return &FailureRouter{client: client}
}

// hookRouteFloor is the confidence floor for this seam. A routing decision
// changes which lever the retry loop pulls, so a weak answer must keep the
// fixed policy rather than redirect the loop.
const hookRouteFloor = 0.7

// HookRouteFloor reports the confidence this router requires.
func (r *FailureRouter) HookRouteFloor() float64 { return hookRouteFloor }

// RouteFailure classifies the hook's reason into the lever it needs.
func (r *FailureRouter) RouteFailure(ctx context.Context, req domainCommit.FailureRouteRequest) (*domainCommit.FailureRoute, error) {
	if r.client == nil {
		return nil, fmt.Errorf("typesafe: no client configured")
	}

	state := map[string]any{
		"purpose": "Classify why a commit message was rejected and which lever fixes it.",
		"rejectedMessage": map[string]any{
			"title":   req.Title,
			"bullets": req.Bullets,
		},
		"rejectedFiles":  capStrings(req.Files, 40),
		"remainingFiles": capStrings(req.RemainingFiles, 40),
	}
	result, err := r.client.Evaluate(ctx, state, map[string]Question{
		"action": Choice(
			map[string]any{
				"hookReason": req.HookReason,
				"judgement":  "Which change fixes this rejection?",
			},
			map[string]string{
				string(domainCommit.FailureRewrite): "The message wording, format, or title is wrong, while the file grouping is right.",
				string(domainCommit.FailureReplan):  "The files are grouped wrongly, so the message cannot describe one coherent change.",
				string(domainCommit.FailureGiveUp):  "Neither fix is appropriate, for example the change itself is rejected.",
			},
		),
	})
	if err != nil {
		return nil, err
	}

	answer, ok := result.Answer("action")
	if !ok || answer.Choice == nil {
		return nil, fmt.Errorf("jev returned no routing answer")
	}
	route := &domainCommit.FailureRoute{
		Action: domainCommit.FailureAction(*answer.Choice),
		Reason: req.HookReason,
	}
	if answer.Confidence != nil {
		route.Confidence = *answer.Confidence
	}
	switch route.Action {
	case domainCommit.FailureRewrite, domainCommit.FailureReplan, domainCommit.FailureGiveUp:
	default:
		return nil, fmt.Errorf("jev returned an unknown action %q", *answer.Choice)
	}
	return route, nil
}

// typeScopeMaxScopes bounds how many scopes one judgment offers. A repository
// with more scopes than this is not judged: the option set would crowd out the
// file evidence.
const typeScopeMaxScopes = 24

// typeScopeFloor is the confidence floor for this seam. It is stricter than the
// policy default because a wrong type or scope reaches the commit history,
// where the conventional hook would otherwise have caught a bad prefix.
const typeScopeFloor = 0.85

// TypeScopeJudge decides the conventional-commit type and scope prefix.
//
// The judgment runs on a structured summary only: changed paths with line
// counts and the symbols the diff touched. The diff body never travels here,
// because the measured accuracy did not need it and it is the expensive and
// private part of the change.
type TypeScopeJudge struct {
	client *Client
}

// NewTypeScopeJudge wires a judge to a client.
func NewTypeScopeJudge(client *Client) *TypeScopeJudge {
	return &TypeScopeJudge{client: client}
}

// scopeFloor lets a confident scope be pinned.
//
// Measurement over a repository's own history put scope agreement at 92 percent
// with every disagreement arriving around 0.5 confidence, so 0.7 separates right
// from wrong. The same measurement found the type agreeing 58 to 67 percent with
// its disagreements arriving at 0.81 to 0.94: no threshold separates those, so the
// type has no floor and is never pinned.
const scopeFloor = 0.7

// ScopeFloor reports the floor the scope answer must reach.
func (j *TypeScopeJudge) ScopeFloor() float64 { return scopeFloor }

// JudgeTypeScope answers with one type and one scope, both chosen from the sets
// code holds.
func (j *TypeScopeJudge) JudgeTypeScope(ctx context.Context, req domainCommit.TypeScopeRequest) (*domainCommit.TypeScopeVerdict, error) {
	if j.client == nil {
		return nil, fmt.Errorf("typesafe: no client configured")
	}
	if len(req.Scopes) == 0 {
		return nil, fmt.Errorf("jev type judgment needs at least one scope to choose from")
	}
	if len(req.Scopes) > typeScopeMaxScopes {
		return nil, fmt.Errorf("jev type judgment received %d scopes, above the %d limit", len(req.Scopes), typeScopeMaxScopes)
	}
	if !domainCommit.JudgeSupportsLanguage(req.Language) {
		return nil, fmt.Errorf("jev type judgment is not measured for language %q", req.Language)
	}

	scopeNames := make([]string, 0, len(req.Scopes))
	for name := range req.Scopes {
		scopeNames = append(scopeNames, name)
	}
	sort.Strings(scopeNames)

	files := req.Files
	if len(files) > fileChangesCap {
		files = files[:fileChangesCap]
	}
	state := map[string]any{
		"purpose":      "Decide the conventional-commit type and scope of this change. Judge only from this state.",
		"changedFiles": files,
		"fileCount":    len(req.Files),
		"scopeCount":   len(req.Scopes),
	}
	if len(req.Symbols) > 0 {
		state["touchedSymbols"] = capStrings(req.Symbols, 20)
	}
	if req.Intent != "" {
		state["intent"] = req.Intent
	}

	questions := map[string]Question{
		"type": Choice(
			"Which conventional-commit type describes this change?",
			typeOptions(),
		),
		"scope": Choice(
			"Which configured scope covers the main content of this change?",
			req.Scopes,
		),
	}
	result, err := j.client.Evaluate(ctx, state, questions)
	if err != nil {
		return nil, err
	}

	verdict := &domainCommit.TypeScopeVerdict{Probabilities: map[string]float64{}}
	typeAnswer, ok := result.Answer("type")
	if !ok || typeAnswer.Choice == nil {
		return nil, fmt.Errorf("jev returned no type answer")
	}
	verdict.Type = *typeAnswer.Choice
	if typeAnswer.Confidence != nil {
		verdict.TypeConfidence = *typeAnswer.Confidence
	}

	scopeAnswer, ok := result.Answer("scope")
	if !ok || scopeAnswer.Choice == nil {
		return nil, fmt.Errorf("jev returned no scope answer")
	}
	verdict.Scope = *scopeAnswer.Choice
	if !contains(scopeNames, verdict.Scope) {
		return nil, fmt.Errorf("jev returned the unoffered scope %q", verdict.Scope)
	}

	for option, probability := range typeAnswer.Probabilities {
		verdict.Probabilities["type:"+option] = probability
	}
	for option, probability := range scopeAnswer.Probabilities {
		verdict.Probabilities["scope:"+option] = probability
	}
	if scopeAnswer.Confidence != nil {
		verdict.ScopeConfidence = *scopeAnswer.Confidence
	}
	return verdict, nil
}

func typeOptions() map[string]string {
	return map[string]string{
		"feat":     "Adds a new user-visible capability or new behavior that did not exist before.",
		"fix":      "Corrects a defect: wrong behavior, a crash, a broken contract, or a regression.",
		"docs":     "Changes documentation only: README, changelog, comments, markdown guides.",
		"refactor": "Restructures or removes code without changing observable behavior.",
		"test":     "Adds or updates tests; no production code changes.",
		"chore":    "Maintenance that is neither a feature, a fix, a refactor, nor documentation: config, tooling, housekeeping.",
		"perf":     "Improves performance without changing behavior.",
		"style":    "Formatting-only change: whitespace or naming.",
		"build":    "Changes build files, dependencies, or packaging.",
		"ci":       "Changes continuous integration configuration or workflows.",
		"revert":   "Reverts a previous commit.",
	}
}
