package application_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fradser/ga-cli/application"
)

// mockLLMClient implements application.LLMClient.
type mockLLMClient struct {
	scopes    []string
	reasoning string
	err       error
}

func (m *mockLLMClient) GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string) ([]string, string, error) {
	return m.scopes, m.reasoning, m.err
}

// mockGitReader implements application.GitReader.
type mockGitReader struct {
	commits   []string
	dirs      []string
	files     []string
	isGitRepo bool
	err       error
}

func (m *mockGitReader) CommitSubjects(ctx context.Context, max int) ([]string, error) {
	return m.commits, m.err
}

func (m *mockGitReader) TopLevelDirs(ctx context.Context) ([]string, error) {
	return m.dirs, m.err
}

func (m *mockGitReader) ProjectFiles(ctx context.Context) ([]string, error) {
	return m.files, m.err
}

func (m *mockGitReader) IsGitRepo(ctx context.Context) bool {
	return m.isGitRepo
}

func TestInitService_WritesProjectYML(t *testing.T) {
	dir := t.TempDir()
	ymlPath := filepath.Join(dir, "project.yml")
	hookPath := filepath.Join(dir, "conventional")

	llm := &mockLLMClient{scopes: []string{"cmd", "application"}, reasoning: "top dirs"}
	git := &mockGitReader{commits: []string{"feat: add init"}, dirs: []string{"cmd", "application"}, isGitRepo: true}
	svc := application.NewInitService(llm, git)

	req := application.InitRequest{
		ProjectYMLPath: ymlPath,
		HookPath:       hookPath,
		HookName:       "conventional",
		Force:          false,
		MaxCommits:     20,
	}
	if err := svc.Init(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(ymlPath); os.IsNotExist(err) {
		t.Fatal("expected project.yml to be created")
	}
}

func TestInitService_ErrorIfProjectYMLExists(t *testing.T) {
	dir := t.TempDir()
	ymlPath := filepath.Join(dir, "project.yml")
	if err := os.WriteFile(ymlPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	llm := &mockLLMClient{scopes: []string{"cmd"}}
	git := &mockGitReader{isGitRepo: true}
	svc := application.NewInitService(llm, git)

	req := application.InitRequest{
		ProjectYMLPath: ymlPath,
		HookPath:       filepath.Join(dir, "conventional"),
		HookName:       "conventional",
		Force:          false,
		MaxCommits:     20,
	}
	if err := svc.Init(context.Background(), req); err == nil {
		t.Fatal("expected error when project.yml exists and Force=false")
	}
}

func TestInitService_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	ymlPath := filepath.Join(dir, "project.yml")
	if err := os.WriteFile(ymlPath, []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}

	llm := &mockLLMClient{scopes: []string{"cmd", "application"}}
	git := &mockGitReader{commits: []string{"fix: bug"}, dirs: []string{"cmd"}, isGitRepo: true}
	svc := application.NewInitService(llm, git)

	req := application.InitRequest{
		ProjectYMLPath: ymlPath,
		HookPath:       filepath.Join(dir, "conventional"),
		HookName:       "conventional",
		Force:          true,
		MaxCommits:     20,
	}
	if err := svc.Init(context.Background(), req); err != nil {
		t.Fatalf("unexpected error with Force=true: %v", err)
	}

	data, err := os.ReadFile(ymlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old content" {
		t.Fatal("expected project.yml to be overwritten")
	}
}

func TestInitService_UnknownHook(t *testing.T) {
	dir := t.TempDir()

	llm := &mockLLMClient{scopes: []string{"cmd"}}
	git := &mockGitReader{isGitRepo: true}
	svc := application.NewInitService(llm, git)

	req := application.InitRequest{
		ProjectYMLPath: filepath.Join(dir, "project.yml"),
		HookPath:       filepath.Join(dir, "unknown-hook"),
		HookName:       "unknown-hook",
		Force:          false,
		MaxCommits:     20,
	}
	if err := svc.Init(context.Background(), req); err == nil {
		t.Fatal("expected error for unknown hook name")
	}
}

func TestInitService_NotGitRepo(t *testing.T) {
	dir := t.TempDir()

	llm := &mockLLMClient{}
	git := &mockGitReader{isGitRepo: false}
	svc := application.NewInitService(llm, git)

	req := application.InitRequest{
		ProjectYMLPath: filepath.Join(dir, "project.yml"),
		HookPath:       filepath.Join(dir, "conventional"),
		HookName:       "conventional",
		Force:          false,
		MaxCommits:     20,
	}
	if err := svc.Init(context.Background(), req); err == nil {
		t.Fatal("expected error when not in a git repo")
	}
}
