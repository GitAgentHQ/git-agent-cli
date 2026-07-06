package graph

import "strings"

// IsToolingPath reports whether a repo-relative path belongs to agent tooling
// metadata rather than user code.
func IsToolingPath(p string) bool {
	for _, dir := range []string{".git-agent", ".claude"} {
		if p == dir || strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// IsTestFile reports whether a repo-relative path is a test file, using
// language-agnostic naming conventions (co-change is computed from git history,
// so the heuristic must not assume any single language). It recognises the
// common separator-delimited suffixes/prefixes and a test/spec path segment,
// while avoiding false positives like "latest.go" or "manifest.json".
func IsTestFile(filePath string) bool {
	lower := strings.ToLower(filePath)
	base := lower
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}

	// Filename conventions: foo_test.go, foo.test.ts, foo.spec.js, foo_spec.rb.
	for _, frag := range []string{"_test.", ".test.", ".spec.", "_spec."} {
		if strings.Contains(base, frag) {
			return true
		}
	}
	// pytest-style prefixes: test_foo.py, spec_foo.rb.
	if strings.HasPrefix(base, "test_") || strings.HasPrefix(base, "spec_") {
		return true
	}

	// Directory conventions: a path segment that is a test/spec folder
	// (e.g. src/test/java/..., app/__tests__/...).
	for _, seg := range []string{"/test/", "/tests/", "/spec/", "/__tests__/", "/testing/"} {
		if strings.Contains("/"+lower, seg) {
			return true
		}
	}
	return false
}
