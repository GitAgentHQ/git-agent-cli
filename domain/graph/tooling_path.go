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
