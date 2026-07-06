package diff

// Rename is a detected file move: the content tracked at Old was removed and
// the same content reappeared at New. Both paths must be committed together so
// git records a rename instead of splitting the move into an orphaned delete
// and add across separate commits.
type Rename struct {
	Old string
	New string
}
