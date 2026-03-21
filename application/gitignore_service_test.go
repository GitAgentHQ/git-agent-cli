package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
)

// --- mocks ---

type mockTechDetector struct {
	techs []string
	err   error
}

func (m *mockTechDetector) DetectTechnologies(_ context.Context, _ domainGitignore.DetectRequest) ([]string, error) {
	return m.techs, m.err
}

type mockContentGenerator struct {
	content string
	err     error
}

func (m *mockContentGenerator) Generate(_ context.Context, _ []string) (string, error) {
	return m.content, m.err
}

// --- helpers ---

func setupGitignoreTest(t *testing.T) (svc *application.GitignoreService, detector *mockTechDetector, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)

	detector = &mockTechDetector{techs: []string{"go", "macos"}}
	generator := &mockContentGenerator{content: "# go rules\n*.o\n"}
	git := &mockGitReader{dirs: []string{"cmd", "domain"}, files: []string{"main.go"}, isGitRepo: true}
	svc = application.NewGitignoreService(detector, generator, git)

	cleanup = func() { os.Chdir(orig) }
	return svc, detector, cleanup
}

// --- tests ---

func TestGitignoreService_Generate_CreatesFile(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	techs, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(techs) == 0 {
		t.Fatal("expected detected technologies")
	}

	data, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf(".gitignore not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "### git-agent auto-generated") {
		t.Error("missing auto-generated start marker")
	}
	if !strings.Contains(content, "### end git-agent ###") {
		t.Error("missing auto-generated end marker")
	}
}

func TestGitignoreService_Generate_UniqueRulesUnderCustomSection(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	// *.o is in the generated content so it should be deduped.
	// my-secret.txt is unique and should appear under ### custom rules ###.
	initial := "my-secret.txt\n*.o\n"
	os.WriteFile(".gitignore", []byte(initial), 0644)

	_, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".gitignore")
	content := string(data)
	if !strings.Contains(content, "my-secret.txt") {
		t.Error("unique custom rule should be preserved")
	}
	if !strings.Contains(content, "### custom rules ###") {
		t.Error("custom section header should be present")
	}
	// *.o appears in generated content, must not be duplicated.
	if strings.Count(content, "*.o") != 1 {
		t.Errorf("*.o should appear exactly once (deduped), got %d", strings.Count(content, "*.o"))
	}
	// Custom rules must appear AFTER ### end git-agent ###.
	endIdx := strings.Index(content, "### end git-agent ###")
	customIdx := strings.Index(content, "### custom rules ###")
	if customIdx < endIdx {
		t.Error("custom rules section should appear after the auto-generated block")
	}
}

func TestGitignoreService_Generate_PreservesRulesFromPreviousBlock(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	// File with previous auto-gen block, custom rules before and after.
	initial := "my-secret.txt\n### git-agent auto-generated — DO NOT EDIT this block ###\n# Technologies: go, macos\n*.o\n### end git-agent ###\n### custom rules ###\nold-custom.txt\n"
	os.WriteFile(".gitignore", []byte(initial), 0644)

	_, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".gitignore")
	content := string(data)
	if !strings.Contains(content, "my-secret.txt") {
		t.Error("rule from before old block should be preserved")
	}
	if !strings.Contains(content, "old-custom.txt") {
		t.Error("rule from old custom section should be preserved")
	}
	// customSection header should appear only once.
	if strings.Count(content, "### custom rules ###") != 1 {
		t.Errorf("### custom rules ### should appear exactly once, got %d", strings.Count(content, "### custom rules ###"))
	}
}

func TestGitignoreService_Generate_ForceOverwrites(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	os.WriteFile(".gitignore", []byte("# my important custom rule\ndo-not-remove.txt\n"), 0644)

	_, err := svc.Generate(context.Background(), application.GitignoreRequest{Force: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".gitignore")
	content := string(data)
	if strings.Contains(content, "do-not-remove.txt") {
		t.Error("force should overwrite custom rules")
	}
	if !strings.Contains(content, "### git-agent auto-generated") {
		t.Error("force output should contain auto-generated block")
	}
}

func TestGitignoreService_Generate_IdempotentCustomSection(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	// Seed file with a previous auto-gen block and custom rules.
	initial := "### git-agent auto-generated — DO NOT EDIT this block ###\n# Technologies: go, macos\n*.o\n### end git-agent ###\n\n### custom rules ###\nmy-rule.txt\n"
	os.WriteFile(".gitignore", []byte(initial), 0644)

	// Run twice; the output must be identical on the second run.
	for i := 0; i < 2; i++ {
		_, err := svc.Generate(context.Background(), application.GitignoreRequest{})
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i+1, err)
		}
	}

	data, _ := os.ReadFile(".gitignore")
	content := string(data)

	// custom section header must appear exactly once.
	if strings.Count(content, "### custom rules ###") != 1 {
		t.Errorf("### custom rules ### should appear exactly once, got %d occurrences", strings.Count(content, "### custom rules ###"))
	}

	// There must be no consecutive blank lines after the custom section header.
	if strings.Contains(content, "### custom rules ###\n\n") {
		t.Error("blank line accumulation detected after ### custom rules ###")
	}
}

func TestGitignoreService_Generate_WritesToCorrectPath(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	_, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(".", ".gitignore")); err != nil {
		t.Errorf(".gitignore not found: %v", err)
	}
}
