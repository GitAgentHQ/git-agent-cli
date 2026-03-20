package commit

// CommitPlan holds a set of grouped file changes that should each become
// a separate atomic commit.
type CommitPlan struct {
	Groups []CommitGroup
}

// CommitGroup represents one logical unit of change.
type CommitGroup struct {
	Files   []string
	Message CommitMessage
}
