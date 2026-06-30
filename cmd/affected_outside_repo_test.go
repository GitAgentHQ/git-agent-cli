package cmd

import (
	"context"
	"os"
	"testing"
)

func TestRejectOutsideRepoAffectedArgs(t *testing.T) {
	root := seedTestRepo(t)
	// runGraphAffected resolves cwd via os.Getwd(), so chdir into the repo root
	// so the relative-path cases land inside the repo.
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"no args is allowed (working-tree mode)", nil, false},
		{"in-repo existing file is accepted", []string{"main.go"}, false},
		{"in-repo nonexistent file is accepted (deleted history)", []string{"pkg/deleted.go"}, false},
		{"absolute path outside repo is rejected", []string{"/nonexistent/file.go"}, true},
		{"system path is rejected", []string{"/etc/hosts"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectOutsideRepoAffectedArgs(context.Background(), tt.args)
			if tt.wantErr && err == nil {
				t.Errorf("rejectOutsideRepoAffectedArgs(%v) = nil, want an error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("rejectOutsideRepoAffectedArgs(%v) = %v, want nil", tt.args, err)
			}
		})
	}
}
