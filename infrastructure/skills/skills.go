// Package skills serves git-agent's own usage documentation, embedded at
// build time so the content always matches the installed binary version.
package skills

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed core.md cli.md
var content embed.FS

// documents is the registry of available documents, in the order shown by
// `skills list`. Keep it in sync with the go:embed patterns above.
var documents = []string{"core", "cli"}

// Names returns the available document names.
func Names() []string {
	return append([]string(nil), documents...)
}

// Get returns the markdown for the named document, or an error listing the
// available documents.
func Get(name string) (string, error) {
	for _, doc := range documents {
		if doc == name {
			data, err := content.ReadFile(name + ".md")
			if err != nil {
				return "", fmt.Errorf("read embedded document %q: %w", name, err)
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("unknown skill document %q (available: %s)", name, strings.Join(documents, ", "))
}
