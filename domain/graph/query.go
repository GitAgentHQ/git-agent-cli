package graph

// ImpactRequest is the input for an impact (co-change) query.
type ImpactRequest struct {
	Path     string
	Depth    int // transitive co-change depth, default 1
	Top      int // max results, default 20
	MinCount int // minimum co-change count, default 3
}

// ImpactResult is the output of an impact query.
type ImpactResult struct {
	Target     string        `json:"target"`
	CoChanged  []ImpactEntry `json:"co_changed"`
	TotalFound int           `json:"total_found"`
	QueryMs    int64         `json:"query_ms"`
}

// ImpactEntry is a single co-changed file in an impact result.
type ImpactEntry struct {
	Path             string  `json:"path"`
	CouplingCount    int     `json:"coupling_count"`
	CouplingStrength float64 `json:"coupling_strength"`
	Depth            int     `json:"depth,omitempty"`
}

// TimelineRequest is the input for a timeline query.
type TimelineRequest struct {
	Since  int64  // unix timestamp, 0 = all
	Source string // filter by source, empty = all
	File   string // filter by file path, empty = all
	Top    int    // max sessions, default 50
}

// TimelineResult is the output of a timeline query.
type TimelineResult struct {
	Sessions      []TimelineSession `json:"sessions"`
	TotalSessions int               `json:"total_sessions"`
	TotalActions  int               `json:"total_actions"`
	QueryMs       int64             `json:"query_ms"`
}

// TimelineSession is a session with its actions in the timeline.
type TimelineSession struct {
	ID          string           `json:"id"`
	Source      string           `json:"source"`
	StartedAt   string           `json:"started_at"` // RFC 3339
	EndedAt     string           `json:"ended_at,omitempty"`
	ActionCount int              `json:"action_count"`
	Actions     []TimelineAction `json:"actions,omitempty"`
}

// TimelineAction is a single action in the timeline.
type TimelineAction struct {
	ID        string   `json:"id"`
	Tool      string   `json:"tool"`
	Timestamp string   `json:"timestamp"` // RFC 3339
	Files     []string `json:"files"`
}
