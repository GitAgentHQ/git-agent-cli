package application_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/project"
)

func TestScopeService_Generate(t *testing.T) {
	llm := &mockLLMClient{scopes: []project.Scope{{Name: "cmd", Description: "CLI commands"}, {Name: "app", Description: "business logic"}}, reasoning: "top dirs"}
	git := &mockGitReader{
		commits:   []string{"feat: init"},
		dirs:      []string{"cmd", "application"},
		isGitRepo: true,
	}
	svc := application.NewScopeService(llm, git)

	scopes, err := svc.Generate(context.Background(), 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d: %v", len(scopes), scopes)
	}
	if scopes[0].Description != "CLI commands" {
		t.Errorf("expected description preserved, got %q", scopes[0].Description)
	}
}

func TestScopeService_Generate_LLMError(t *testing.T) {
	llm := &mockLLMClient{err: errors.New("llm down")}
	git := &mockGitReader{isGitRepo: true}
	svc := application.NewScopeService(llm, git)

	_, err := svc.Generate(context.Background(), 20)
	if err == nil {
		t.Fatal("expected error from LLM failure, got nil")
	}
}

func TestScopeService_MergeAndSave_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.yml")

	svc := application.NewScopeService(nil, nil)
	if err := svc.MergeAndSave(context.Background(), path, []project.Scope{{Name: "cmd"}, {Name: "app"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected file to be created")
	}
}

func TestScopeService_MergeAndSave_DeduplicatesScopes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.yml")

	svc := application.NewScopeService(nil, nil)

	if err := svc.MergeAndSave(context.Background(), path, []project.Scope{{Name: "cmd"}, {Name: "app"}}); err != nil {
		t.Fatalf("unexpected error on first write: %v", err)
	}
	// "app" is duplicate, "infra" is new
	if err := svc.MergeAndSave(context.Background(), path, []project.Scope{{Name: "app"}, {Name: "infra"}}); err != nil {
		t.Fatalf("unexpected error on second write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"cmd", "app", "infra"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in merged config, got:\n%s", want, content)
		}
	}

	// "app" should appear exactly once
	if strings.Count(content, "app") != 1 {
		t.Errorf("expected 'app' exactly once, got:\n%s", content)
	}
}

func TestScopeService_MergeAndSave_CaseInsensitiveDedupe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.yml")

	svc := application.NewScopeService(nil, nil)

	if err := svc.MergeAndSave(context.Background(), path, []project.Scope{{Name: "CMD"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.MergeAndSave(context.Background(), path, []project.Scope{{Name: "cmd"}}); err != nil {
		t.Fatalf("unexpected error on second write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Should have exactly one scope entry (CMD or cmd but not both)
	count := strings.Count(strings.ToLower(string(data)), "cmd")
	if count != 1 {
		t.Errorf("expected exactly 1 'cmd' entry after case-insensitive dedupe, got %d in:\n%s", count, data)
	}
}
