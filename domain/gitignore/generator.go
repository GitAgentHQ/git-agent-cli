package gitignore

import "context"

// ContentGenerator generates .gitignore content from a list of technology identifiers.
type ContentGenerator interface {
	Generate(ctx context.Context, technologies []string) (string, error)
}
