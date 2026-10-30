package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gitagenthq/git-agent/domain/decision"
	"github.com/gitagenthq/git-agent/domain/project"
)

type ScopeService struct {
	llm LLMClient
	git GitReader
	// decider decides which scope covers which directory. A nil decider leaves
	// that decision to the model, which is the behaviour before the Jev layer
	// existed and the behaviour when the layer is switched off.
	decider project.ScopeDecider
	// warn receives verbose messages about a fallback from the decider to the
	// model. A nil writer drops them.
	warn io.Writer
	// decisions carries the System One policy. Nil leaves the layer off.
	decisions *Decisions
}

func NewScopeService(llm LLMClient, git GitReader, decider project.ScopeDecider, warn io.Writer) *ScopeService {
	return &ScopeService{llm: llm, git: git, decider: decider, warn: warn}
}

// WithDecisions attaches the System One layer for one run.
func (s *ScopeService) WithDecisions(d *Decisions) *ScopeService {
	s.decisions = d
	return s
}

// evidence is the repository material both scope paths judge: the commit log,
// the top-level directories, and the tracked files.
type evidence struct {
	commits []string
	dirs    []string
	files   []string
}

func (s *ScopeService) Generate(ctx context.Context, maxCommits int, existingScopes []project.Scope) ([]project.Scope, error) {
	ev, err := s.readEvidence(ctx, maxCommits)
	if err != nil {
		return nil, err
	}

	session := s.session()
	var shadow *pendingScopeJudgment
	if s.decider != nil && session.Take(SeamScopeSet) {
		scopes, pending, err := s.decideScopes(ctx, session, ev, existingScopes)
		switch {
		case err == nil:
			return filterConventionalTypes(scopes), nil
		case errors.Is(err, errNotAdopted):
			// The judgment ran but did not reach the floor. Measure it below
			// against the model's answer instead of applying it.
			shadow = pending
		default:
			// A cancelled context is the caller leaving, not a decider failure,
			// so it must not start a second model call.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if session.Mode() == decision.ModeOn {
				s.warnf("scope decision unavailable (%v); asking the model instead", err)
			}
		}
	}

	if s.llm == nil {
		return nil, fmt.Errorf("no scope provider: the decision layer did not decide and no model is wired")
	}
	scopes, _, err := s.llm.GenerateScopes(ctx, ev.commits, ev.dirs, ev.files, existingScopes)
	if err != nil {
		return nil, fmt.Errorf("generating scopes: %w", err)
	}

	// A shadow run compares the unadopted judgment against the answer the model
	// just gave, which is the measurement that promotes a seam to the on mode.
	if shadow != nil {
		shadow.Observation.Baseline = scopeNames(scopes)
		shadow.Observation.Agreement = agreementBetween(shadow.Observation.Decision, shadow.Observation.Baseline)
		session.Report(shadow.started, shadow.Observation)
	}

	return filterConventionalTypes(scopes), nil
}

// session returns the layer for one run, or nil when the layer is off.
func (s *ScopeService) session() *Session {
	return s.decisions.Begin()
}

// pendingScopeJudgment carries a shadowed verdict until the model's answer
// exists to compare it with.
type pendingScopeJudgment struct {
	started     time.Time
	Observation decision.Observation
}

// agreementBetween compares two comma-separated name lists as sets.
func agreementBetween(decided, baseline string) decision.Agreement {
	if decided == "" || baseline == "" {
		return decision.AgreementNone
	}
	if sameNameSet(decided, baseline) {
		return decision.AgreementAgree
	}
	return decision.AgreementDisagree
}

func sameNameSet(a, b string) bool {
	setA, setB := nameSet(a), nameSet(b)
	if len(setA) != len(setB) {
		return false
	}
	for name := range setA {
		if !setB[name] {
			return false
		}
	}
	return true
}

func nameSet(list string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(list, ",") {
		if name := strings.ToLower(strings.TrimSpace(part)); name != "" {
			out[name] = true
		}
	}
	return out
}

func scopeNames(scopes []project.Scope) string {
	names := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		names = append(names, sc.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func (s *ScopeService) readEvidence(ctx context.Context, maxCommits int) (evidence, error) {
	var ev evidence

	commits, err := s.git.CommitLog(ctx, maxCommits)
	if err != nil {
		return ev, fmt.Errorf("reading commit log: %w", err)
	}
	dirs, err := s.git.TopLevelDirs(ctx)
	if err != nil {
		return ev, fmt.Errorf("reading dirs: %w", err)
	}
	files, err := s.git.ProjectFiles(ctx)
	if err != nil {
		return ev, fmt.Errorf("reading project files: %w", err)
	}

	ev.commits, ev.dirs, ev.files = commits, dirs, files
	return ev, nil
}

// decideScopes applies the decider's verdict and asks the model to word each
// new scope. The decider owns which scopes exist; the model owns only their
// wording, and a description failure leaves the scope in place without one
// because a description is optional.
//
// Outside the on mode the verdict is measured, not applied: it comes back as
// errNotAdopted together with the observation to compare, and the caller then
// asks the model for the scope set.
func (s *ScopeService) decideScopes(ctx context.Context, session *Session, ev evidence, existingScopes []project.Scope) ([]project.Scope, *pendingScopeJudgment, error) {
	started := time.Now()
	verdict, err := s.decider.DecideScopes(ctx, project.ScopeDecisionRequest{
		Dirs:           ev.dirs,
		Files:          ev.files,
		Commits:        ev.commits,
		ExistingScopes: existingScopes,
	})
	if err != nil {
		session.Report(started, decision.Observation{Seam: SeamScopeSet, Err: err.Error()})
		return nil, nil, err
	}

	proposed := verdict.Created()
	decided := proposedNames(proposed)
	if !session.Adopt(SeamScopeSet, verdict.Confidence) {
		pending := &pendingScopeJudgment{
			started: started,
			Observation: decision.Observation{
				Seam:       SeamScopeSet,
				Decision:   decided,
				Confidence: verdict.Confidence,
			},
		}
		return nil, pending, errNotAdopted
	}

	scopes := make([]project.Scope, 0, len(proposed))
	for _, created := range proposed {
		scopes = append(scopes, project.Scope{Name: created.Name})
	}
	session.Report(started, decision.Observation{
		Seam:       SeamScopeSet,
		Decision:   decided,
		Adopted:    true,
		Confidence: verdict.Confidence,
	})

	described, err := s.describe(ctx, session, scopes, proposed)
	if err != nil {
		return nil, nil, err
	}
	return described, nil, nil
}

// errNotAdopted is the internal signal that a verdict ran but did not reach the
// confidence floor. It never reaches the caller: the caller falls back to the
// model either way.
var errNotAdopted = errors.New("scope decision not adopted")

// describe asks the model to word the scopes the judgment selected. A failure
// leaves the scope without a description, because a description is optional and
// a missing one must not cost the decision.
func (s *ScopeService) describe(ctx context.Context, session *Session, scopes []project.Scope, proposed []project.ProposedScope) ([]project.Scope, error) {
	if s.llm == nil || len(scopes) == 0 {
		return scopes, nil
	}
	described, err := s.llm.DescribeScopes(ctx, proposed)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		session.Report(time.Now(), decision.Observation{Seam: SeamScopeSet + ".describe", Err: err.Error()})
		return scopes, nil
	}
	return applyDescriptions(scopes, described), nil
}

func proposedNames(proposed []project.ProposedScope) string {
	names := make([]string, 0, len(proposed))
	for _, p := range proposed {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// applyDescriptions copies each description onto the scope with the same name.
// A scope the model omitted or renamed keeps its empty description.
func applyDescriptions(scopes, described []project.Scope) []project.Scope {
	if len(described) == 0 {
		return scopes
	}
	byName := make(map[string]string, len(described))
	for _, d := range described {
		if d.Name != "" && d.Description != "" {
			byName[strings.ToLower(d.Name)] = d.Description
		}
	}
	for i := range scopes {
		if scopes[i].Description != "" {
			continue
		}
		if desc, ok := byName[strings.ToLower(scopes[i].Name)]; ok {
			scopes[i].Description = desc
		}
	}
	return scopes
}

func (s *ScopeService) warnf(format string, args ...any) {
	if s.warn == nil {
		return
	}
	fmt.Fprintf(s.warn, format+"\n", args...)
}

// ReadScopes reads existing scopes from a YAML config file.
func ReadScopes(path string) []project.Scope {
	rawMap := readExistingYAMLMap(path)
	if v, ok := rawMap["scopes"]; ok {
		return parseScopesFromYAML(v)
	}
	return nil
}

// conventionalTypes is the standard set of conventional commit types.
// Scopes must not duplicate these names.
var conventionalTypes = map[string]bool{
	"build": true, "ci": true, "docs": true, "feat": true, "fix": true,
	"perf": true, "refactor": true, "style": true, "test": true,
	"chore": true, "revert": true,
}

func filterConventionalTypes(scopes []project.Scope) []project.Scope {
	result := scopes[:0:0]
	for _, s := range scopes {
		if !conventionalTypes[strings.ToLower(s.Name)] {
			result = append(result, s)
		}
	}
	return result
}

func (s *ScopeService) MergeAndSave(ctx context.Context, path string, newScopes []project.Scope) ([]project.Scope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Read full YAML map to preserve all existing keys (e.g., hook).
	rawMap := readExistingYAMLMap(path)

	var existingScopes []project.Scope
	if v, ok := rawMap["scopes"]; ok {
		existingScopes = parseScopesFromYAML(v)
	}

	merged, added := mergeScopes(existingScopes, newScopes)
	if len(added) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rawMap["scopes"] = merged

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating config dir: %w", err)
	}

	data, err := yaml.Marshal(rawMap)
	if err != nil {
		return nil, fmt.Errorf("marshalling yaml: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, err
	}

	return added, nil
}

// parseScopesFromYAML handles both legacy string format and new structured format.
func parseScopesFromYAML(v any) []project.Scope {
	switch sv := v.(type) {
	case []interface{}:
		var scopes []project.Scope
		for _, item := range sv {
			switch val := item.(type) {
			case string:
				scopes = append(scopes, project.Scope{Name: val})
			case map[string]interface{}:
				s := project.Scope{}
				if name, ok := val["name"].(string); ok {
					s.Name = name
				}
				if desc, ok := val["description"].(string); ok {
					s.Description = desc
				}
				if s.Name != "" {
					scopes = append(scopes, s)
				}
			}
		}
		return scopes
	}
	return nil
}

func readExistingYAMLMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]any)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil || m == nil {
		return make(map[string]any)
	}
	return m
}

func mergeScopes(existing, newScopes []project.Scope) ([]project.Scope, []project.Scope) {
	seen := make(map[string]int, len(existing))
	for i, s := range existing {
		seen[strings.ToLower(s.Name)] = i
	}
	result := make([]project.Scope, len(existing))
	copy(result, existing)
	var added []project.Scope
	for _, s := range newScopes {
		key := strings.ToLower(s.Name)
		if idx, ok := seen[key]; ok {
			// Update description if the existing one is empty and the new one has one.
			if result[idx].Description == "" && s.Description != "" {
				result[idx].Description = s.Description
			}
		} else {
			result = append(result, s)
			seen[key] = len(result) - 1
			added = append(added, s)
		}
	}
	return result, added
}
