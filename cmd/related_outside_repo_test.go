package cmd

import (
	"path/filepath"
	"testing"
)

func TestRejectOutsideRepoArgs(t *testing.T) {
	root := seedTestRepo(t)
	cwd := root

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"no args is allowed (working-tree mode)", nil, false},
		{"in-repo existing file is accepted", []string{"main.go"}, false},
		{"in-repo nonexistent file is accepted (deleted history)", []string{"pkg/deleted.go"}, false},
		{"absolute path outside repo is rejected", []string{"/nonexistent/path"}, true},
		{"absolute path outside repo is rejected (file.go)", []string{"/nonexistent/file.go"}, true},
		{"system path is rejected", []string{"/etc/hosts"}, true},
		{"dotdot escape is rejected", []string{filepath.Join("..", "outside.go")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectOutsideRepoArgs(root, cwd, tt.args)
			if tt.wantErr && err == nil {
				t.Errorf("rejectOutsideRepoArgs(%v) = nil, want an error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("rejectOutsideRepoArgs(%v) = %v, want nil", tt.args, err)
			}
		})
	}
}
