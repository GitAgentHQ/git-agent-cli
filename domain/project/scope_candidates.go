package project

import "strings"

// stdDirAbbrevs maps a top-level directory name to the short scope name this
// project uses for it. It is the single source of truth for scope naming: the
// decider derives new scope names from it, and the CLI coverage check compares
// directory names against it.
var stdDirAbbrevs = map[string]string{
	"application":    "app",
	"infrastructure": "infra",
	"cmd":            "cli",
	"command":        "cli",
	"documentation":  "docs",
	"test":           "tests",
}

// skipDirNames lists top-level directories that never justify a scope. The
// entries are the build output, dependency, and asset directories the scope
// generation contract already excludes, kept here so candidate generation does
// not depend on the model following that rule in prose.
var skipDirNames = map[string]bool{
	"node_modules":  true,
	"vendor":        true,
	"dist":          true,
	"build":         true,
	"out":           true,
	"target":        true,
	"coverage":      true,
	"__pycache__":   true,
	".next":         true,
	".git":          true,
	"docs":          true,
	"doc":           true,
	"documentation": true,
	"assets":        true,
	"static":        true,
	"public":        true,
	"resources":     true,
	"examples":      true,
	"example":       true,
	"testdata":      true,
	"tmp":           true,
	"temp":          true,
	"logs":          true,
}

// DeriveScopeName returns the short scope name for a top-level directory.
// Directories with a known short form use it; every other name is returned
// lowercased with separators removed, so the result is always a single token.
func DeriveScopeName(dir string) string {
	name := strings.ToLower(strings.Trim(dir, "/"))
	if name == "" {
		return ""
	}
	if abbrev, ok := stdDirAbbrevs[name]; ok {
		return abbrev
	}
	// A nested path such as "git-agent/pi" reduces to its last segment.
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.NewReplacer("-", "", "_", "", " ", "").Replace(name)
	return strings.ToLower(name)
}

// CandidateDirs filters top-level directory names down to the ones a decider
// should judge: dot-directories, build output, dependencies, and asset
// directories are removed. Order is preserved so the candidate set, and any
// model-visible wording derived from it, stays stable across runs.
func CandidateDirs(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		name := strings.ToLower(strings.Trim(strings.TrimSpace(d), "/"))
		if name == "" || seen[name] {
			continue
		}
		if strings.HasPrefix(name, ".") || skipDirNames[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
