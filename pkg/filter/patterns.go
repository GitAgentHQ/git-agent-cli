package filter

import (
	"path/filepath"
)

var filteredNames = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"go.sum":            true,
	"Gemfile.lock":      true,
}

var filteredExts = map[string]bool{
	".png":   true,
	".jpg":   true,
	".gif":   true,
	".ico":   true,
	".pdf":   true,
	".woff":  true,
	".woff2": true,
	".ttf":   true,
	".eot":   true,
}

func IsFiltered(filename string) bool {
	base := filepath.Base(filename)
	if filteredNames[base] {
		return true
	}
	return filteredExts[filepath.Ext(base)]
}
