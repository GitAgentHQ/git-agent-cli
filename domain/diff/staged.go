package diff

// StagedDiff represents the staged changes in a git repository.
type StagedDiff struct {
	Files   []string
	Content string
	Lines   int
}
