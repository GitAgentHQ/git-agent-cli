package application_test

import (
	"context"
	"testing"

	"github.com/fradser/git-agent/application"
)

type mockGitClient struct {
	calledWith []string
	err        error
}

func (m *mockGitClient) Add(ctx context.Context, paths []string) error {
	m.calledWith = paths
	return m.err
}

func TestAddService_Add_specificPaths(t *testing.T) {
	mock := &mockGitClient{}
	svc := application.NewAddService(mock)

	paths := []string{"src/main.go", "src/util.go"}
	if err := svc.Add(context.Background(), paths); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.calledWith) != len(paths) {
		t.Fatalf("expected git.Add called with %d paths, got %d", len(paths), len(mock.calledWith))
	}
	for i, p := range paths {
		if mock.calledWith[i] != p {
			t.Errorf("path[%d]: expected %q, got %q", i, p, mock.calledWith[i])
		}
	}
}

func TestAddService_Add_dot(t *testing.T) {
	mock := &mockGitClient{}
	svc := application.NewAddService(mock)

	if err := svc.Add(context.Background(), []string{"."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.calledWith) != 1 || mock.calledWith[0] != "." {
		t.Errorf("expected git.Add called with [\".\"], got %v", mock.calledWith)
	}
}
