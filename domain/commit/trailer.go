package commit

// Trailer is a git commit trailer (e.g. "Co-Authored-By: Alice <alice@example.com>").
type Trailer struct {
	Key   string // e.g. "Co-Authored-By", "Signed-off-by"
	Value string // e.g. "Alice <alice@example.com>"
}
