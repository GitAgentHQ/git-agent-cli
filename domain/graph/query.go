package graph

// ImpactRequest is the input for an impact (co-change) query. One or more seed
// paths describe the feature being changed; their co-change neighbours are
// aggregated so files coupled to several seeds rank highest.
type ImpactRequest struct {
	Paths    []string
	Depth    int // transitive co-change depth, default 1
	Top      int // max results, default 20
	MinCount int // minimum co-change count, default 3
	// IncludeCommits attaches, to each result entry, the commits that link it to
	// the seeds (the "why are these related?" evidence). Off by default so callers
	// that only need the coupled paths don't pay for the extra per-entry lookups.
	IncludeCommits bool
}

// ImpactResult is the output of an impact query.
type ImpactResult struct {
	Targets    []string      `json:"targets"`
	CoChanged  []ImpactEntry `json:"co_changed"`
	TotalFound int           `json:"total_found"`
	QueryMs    int64         `json:"query_ms"`
}

// ImpactEntry is a single co-changed file in an impact result.
type ImpactEntry struct {
	Path             string   `json:"path"`
	CouplingCount    int      `json:"coupling_count"`    // total co-change events across matched seeds
	CouplingStrength float64  `json:"coupling_strength"` // strongest single coupling to a seed
	Score            float64  `json:"score"`             // ranking score: sum of strengths over matched seeds
	SeedMatches      int      `json:"seed_matches"`      // how many seed files this file co-changes with
	RelatedTo        []string `json:"related_to,omitempty"`
	Depth            int      `json:"depth,omitempty"`
	// LinkingCommits carries the commits that bind this file to the seed(s) —
	// the "why are these related?" evidence (subject + sha + timestamp). Only
	// populated for top-ranked entries, most-recent first.
	LinkingCommits []CommitRef `json:"commits,omitempty"`
}

// CommitRef is a lightweight reference to a commit that links a co-change pair:
// its hash, the first line of its message (subject), and its timestamp.
type CommitRef struct {
	Hash      string `json:"sha"`
	Subject   string `json:"subject"`
	Timestamp int64  `json:"ts"`
}
