package graph

import "testing"

func TestIsTestFile_LanguageAgnostic(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// Suffix conventions across languages.
		{"auth/token_test.go", true},
		{"src/auth.test.ts", true},
		{"src/auth.spec.js", true},
		{"lib/parser_spec.rb", true},
		// Prefix conventions.
		{"tests/test_auth.py", true},
		{"spec_helper.rb", true},
		// Directory conventions.
		{"src/test/java/com/Foo.java", true},
		{"app/__tests__/widget.jsx", true},
		{"project/tests/runner.go", true},
		// Non-test files that look superficially similar must NOT match.
		{"cmd/latest.go", false},
		{"config/manifest.json", false},
		{"auth/token.go", false},
		{"internal/contest.py", false},
		{"README.md", false},
	}
	for _, tt := range tests {
		if got := IsTestFile(tt.path); got != tt.want {
			t.Errorf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
