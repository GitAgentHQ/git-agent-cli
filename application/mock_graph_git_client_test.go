package application_test

import (
	"context"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// mockGraphGitClient implements graph.GraphGitClient for testing.
type mockGraphGitClient struct {
	diffNameOnlyResult []string
	diffNameOnlyErr    error

	hashObjectResults map[string]string // path -> hash
	hashObjectErr     error

	diffForFilesResult string
	diffForFilesErr    error

	// Unused by capture tests but required by the interface.
	commitLogResult        []graph.CommitInfo
	commitLogErr           error
	currentHeadResult      string
	currentHeadErr         error
	mergeBaseIsAncestorVal bool
	mergeBaseIsAncestorErr error
}

func (m *mockGraphGitClient) DiffNameOnly(_ context.Context) ([]string, error) {
	return m.diffNameOnlyResult, m.diffNameOnlyErr
}

func (m *mockGraphGitClient) HashObject(_ context.Context, filePath string) (string, error) {
	if m.hashObjectErr != nil {
		return "", m.hashObjectErr
	}
	if h, ok := m.hashObjectResults[filePath]; ok {
		return h, nil
	}
	return "unknown", nil
}

func (m *mockGraphGitClient) DiffForFiles(_ context.Context, _ []string) (string, error) {
	return m.diffForFilesResult, m.diffForFilesErr
}

func (m *mockGraphGitClient) CommitLogDetailed(_ context.Context, _ string, _ int) ([]graph.CommitInfo, error) {
	return m.commitLogResult, m.commitLogErr
}

func (m *mockGraphGitClient) CurrentHead(_ context.Context) (string, error) {
	return m.currentHeadResult, m.currentHeadErr
}

func (m *mockGraphGitClient) MergeBaseIsAncestor(_ context.Context, _, _ string) (bool, error) {
	return m.mergeBaseIsAncestorVal, m.mergeBaseIsAncestorErr
}
