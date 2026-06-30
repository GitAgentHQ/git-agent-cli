package application

import (
	"context"
	"sort"

	"github.com/gitagenthq/git-agent/domain/commit"
	"github.com/gitagenthq/git-agent/domain/graph"
)

const maxCoChangeHints = 20

// maxHintSubjects caps how many commit subjects accompany a single co-change
// hint — enough to convey the semantic reason for the coupling without flooding
// the planner prompt.
const maxHintSubjects = 2

// linkingSubjects projects up to n commit subjects from the linking commits,
// skipping blanks.
func linkingSubjects(commits []graph.CommitRef, n int) []string {
	var out []string
	for _, c := range commits {
		if c.Subject == "" {
			continue
		}
		out = append(out, c.Subject)
		if len(out) >= n {
			break
		}
	}
	return out
}

// GraphCoChangeProvider queries co-change data from the graph repository
// and returns hints for files that frequently change together.
type GraphCoChangeProvider struct {
	repo graph.GraphRepository
}

// NewGraphCoChangeProvider creates a GraphCoChangeProvider backed by the given repository.
func NewGraphCoChangeProvider(repo graph.GraphRepository) *GraphCoChangeProvider {
	return &GraphCoChangeProvider{repo: repo}
}

// GetHintsForFiles returns co-change hints for pairs where BOTH files are in the
// provided list. Results are deduplicated, sorted by strength descending, and
// capped at maxCoChangeHints.
func (p *GraphCoChangeProvider) GetHintsForFiles(ctx context.Context, files []string) ([]commit.CoChangeHint, error) {
	if len(files) < 2 {
		return nil, nil
	}

	fileSet := make(map[string]bool, len(files))
	for _, f := range files {
		fileSet[f] = true
	}

	// Track seen pairs to deduplicate (A<->B appears once).
	type pair struct{ a, b string }
	seen := make(map[pair]bool)
	var hints []commit.CoChangeHint

	for _, f := range files {
		result, err := p.repo.Impact(ctx, graph.ImpactRequest{
			Paths:          []string{f},
			Depth:          1,
			Top:            5,
			MinCount:       3,
			IncludeCommits: true,
		})
		if err != nil {
			return nil, err
		}
		for _, entry := range result.CoChanged {
			if !fileSet[entry.Path] {
				continue
			}
			// Normalize pair ordering for dedup.
			a, b := f, entry.Path
			if a > b {
				a, b = b, a
			}
			key := pair{a, b}
			if seen[key] {
				continue
			}
			seen[key] = true
			hints = append(hints, commit.CoChangeHint{
				FileA:    a,
				FileB:    b,
				Strength: entry.CouplingStrength,
				Subjects: linkingSubjects(entry.LinkingCommits, maxHintSubjects),
			})
		}
	}

	sort.Slice(hints, func(i, j int) bool {
		return hints[i].Strength > hints[j].Strength
	})

	if len(hints) > maxCoChangeHints {
		hints = hints[:maxCoChangeHints]
	}

	return hints, nil
}
