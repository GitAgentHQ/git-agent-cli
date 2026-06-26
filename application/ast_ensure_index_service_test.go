//go:build cgo

package application

import (
	"context"
	"path/filepath"
	"testing"

	infraExtraction "github.com/gitagenthq/git-agent/infrastructure/extraction"
)

type fakeASTIndexGit struct {
	fakeTrackedFiles
	head             string
	indexedHead      string
	workingChanges   []string
	committedChanges []string
	ancestorOK       bool
}

func (f *fakeASTIndexGit) CurrentHead(context.Context) (string, error) {
	return f.head, nil
}

func (f *fakeASTIndexGit) DiffNameOnly(context.Context) ([]string, error) {
	return f.workingChanges, nil
}

func (f *fakeASTIndexGit) DiffNameOnlySince(context.Context, string) ([]string, error) {
	return f.committedChanges, nil
}

func (f *fakeASTIndexGit) MergeBaseIsAncestor(context.Context, string, string) (bool, error) {
	return f.ancestorOK, nil
}

type fakeASTIndexState struct {
	values map[string]string
}

func (f *fakeASTIndexState) GetIndexState(_ context.Context, key string) (string, error) {
	if f.values == nil {
		return "", nil
	}
	return f.values[key], nil
}

func (f *fakeASTIndexState) SetIndexState(_ context.Context, key, value string) error {
	if f.values == nil {
		f.values = make(map[string]string)
	}
	f.values[key] = value
	return nil
}

func TestASTEnsureIndexService_IncrementalIndexesChangedFileOnly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), `package main

func Alpha() {}
`)
	writeTestFile(t, filepath.Join(root, "b.go"), `package main

func Beta() {}
`)

	repo, cleanup := setupIndexTestRepo(t)
	defer cleanup()
	extractor := infraExtraction.NewTreeSitterExtractor("go", infraExtraction.GoExtractor())
	idxSvc := NewASTIndexService(repo, fakeTrackedFiles{files: []string{"a.go", "b.go"}}, extractor)
	if _, err := idxSvc.IndexAll(ctx, root); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	state := &fakeASTIndexState{values: map[string]string{astIndexHeadKey: "old-head"}}
	git := &fakeASTIndexGit{
		fakeTrackedFiles: fakeTrackedFiles{files: []string{"a.go", "b.go"}},
		head:             "new-head",
		workingChanges:   []string{"a.go"},
		ancestorOK:       true,
	}
	svc := NewASTEnsureIndexService(repo, state, git, extractor)
	if err := svc.Ensure(ctx, root, "Beta", false, nil); err != nil {
		t.Fatalf("ensure for symbol: %v", err)
	}
	if state.values[astIndexHeadKey] != "new-head" {
		t.Fatalf("ast_index_head = %q, want new-head", state.values[astIndexHeadKey])
	}

	nodes, err := repo.GetASTNodeByName(ctx, "Beta")
	if err != nil {
		t.Fatalf("lookup Beta: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected Beta to remain indexed, got %+v", nodes)
	}
}

func TestASTEnsureIndexService_FallsBackToFullIndexWhenSymbolMissing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), `package main

func Target() {}
`)

	repo, cleanup := setupIndexTestRepo(t)
	defer cleanup()
	extractor := infraExtraction.NewTreeSitterExtractor("go", infraExtraction.GoExtractor())
	state := &fakeASTIndexState{values: map[string]string{astIndexHeadKey: "head-1"}}
	git := &fakeASTIndexGit{
		fakeTrackedFiles: fakeTrackedFiles{files: []string{"a.go"}},
		head:             "head-1",
		ancestorOK:       true,
	}
	svc := NewASTEnsureIndexService(repo, state, git, extractor)
	if err := svc.Ensure(ctx, root, "Target", false, nil); err != nil {
		t.Fatalf("ensure for symbol: %v", err)
	}

	nodes, err := repo.GetASTNodeByName(ctx, "Target")
	if err != nil {
		t.Fatalf("lookup Target: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected full index fallback to find Target, got %+v", nodes)
	}
}
