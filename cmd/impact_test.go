package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRepoPath(t *testing.T) {
	// Use a real directory so symlink resolution (e.g. macOS /var -> /private/var,
	// or git reporting a different root than the caller's cwd) is exercised.
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		cwd    string
		target string
		want   string
	}{
		{"plain repo-relative from root", root, "src/auth.go", "src/auth.go"},
		{"dot-slash prefix", root, "./src/auth.go", "src/auth.go"},
		{"absolute path under root", root, filepath.Join(root, "src/auth.go"), "src/auth.go"},
		{"relative from subdirectory", srcDir, "auth.go", "src/auth.go"},
		{"dot-slash from subdirectory", srcDir, "./auth.go", "src/auth.go"},
		{"redundant separators", root, "src//auth.go", "src/auth.go"},
		{"nonexistent file under root still maps", root, "src/deleted.go", "src/deleted.go"},
		{"path outside repo left untouched", root, "/etc/hosts", "/etc/hosts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRepoPath(root, tt.cwd, tt.target)
			if got != tt.want {
				t.Errorf("normalizeRepoPath(root, %q, %q) = %q, want %q", tt.cwd, tt.target, got, tt.want)
			}
		})
	}
}
