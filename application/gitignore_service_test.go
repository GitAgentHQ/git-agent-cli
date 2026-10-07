package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/application"
	"github.com/gitagenthq/git-agent/domain/decision"
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

	techs, modified, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified to be true when file created")
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

func TestGitignoreService_Generate_HonorsCanceledContextBeforeWrite(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := svc.Generate(ctx, application.GitignoreRequest{}); err == nil {
		t.Fatal("expected canceled context error")
	}
	if _, err := os.Stat(".gitignore"); !os.IsNotExist(err) {
		t.Fatalf("expected canceled write to leave no .gitignore, stat error: %v", err)
	}
}

func TestGitignoreService_Generate_UniqueRulesUnderCustomSection(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	// *.o is in the generated content so it should be deduped.
	// my-secret.txt is unique and should appear under ### custom rules ###.
	initial := "my-secret.txt\n*.o\n"
	os.WriteFile(".gitignore", []byte(initial), 0644)

	_, _, err := svc.Generate(context.Background(), application.GitignoreRequest{})
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

	_, _, err := svc.Generate(context.Background(), application.GitignoreRequest{})
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

func TestGitignoreService_Generate_PreservesCustomRulesOnRegen(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	// Custom rules must survive a regeneration regardless of flags.
	os.WriteFile(".gitignore", []byte("# my important custom rule\ndo-not-remove.txt\n"), 0644)

	_, _, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(".gitignore")
	content := string(data)
	if !strings.Contains(content, "do-not-remove.txt") {
		t.Error("custom rules must be preserved after regeneration")
	}
	if !strings.Contains(content, "### git-agent auto-generated") {
		t.Error("auto-generated block must be present")
	}
	// Custom rules must appear after the auto-generated block.
	endIdx := strings.Index(content, "### end git-agent ###")
	customIdx := strings.Index(content, "do-not-remove.txt")
	if customIdx < endIdx {
		t.Error("custom rule should appear after the auto-generated block")
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
		_, _, err := svc.Generate(context.Background(), application.GitignoreRequest{})
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

// TestGitignoreService_Generate_IgnoresLocalConfig ensures personal overrides
// are consistently ignored across generated files.
func TestGitignoreService_Generate_IgnoresLocalConfig(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	if _, _, err := svc.Generate(context.Background(), application.GitignoreRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf(".gitignore not created: %v", err)
	}
	if !strings.Contains(string(data), ".git-agent/config.local.yml") {
		t.Errorf("generated .gitignore must ignore local configuration:\n%s", data)
	}
}
func TestGitignoreService_Generate_WritesToCorrectPath(t *testing.T) {
	svc, _, cleanup := setupGitignoreTest(t)
	defer cleanup()

	_, _, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(".", ".gitignore")); err != nil {
		t.Errorf(".gitignore not found: %v", err)
	}
}

func TestGitignoreService_TechClassifierIsMeasuredButNotAdopted(t *testing.T) {
	// Given a classifier that publishes the measured floor, which no answer can
	// reach, and a detector that can answer.
	classifier := &measuredTechClassifier{verdict: &domainGitignore.TechnologyVerdict{
		Technologies: []string{"go"}, Confidence: 0.99,
	}}
	dir := t.TempDir()
	git := &mockGitReader{dirs: []string{"application"}, files: []string{"main.go"}, repoRoot: dir}
	svc := application.NewGitignoreService(&mockTechDetector{techs: []string{"go"}}, &mockContentGenerator{}, git).
		WithTechnologyClassifier(classifier).
		WithDecisions(application.NewDecisions(decision.ModeOn, 0.5, 8, nil, nil))

	techs, _, err := svc.Generate(context.Background(), application.GitignoreRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The detector keeps the decision; the classifier ran and decided nothing.
	if classifier.calls != 1 {
		t.Errorf("expected the classifier to run once, got %d calls", classifier.calls)
	}
	if len(techs) == 0 || techs[0] != "go" {
		t.Errorf("expected the detector's technology, got %v", techs)
	}
}

// measuredTechClassifier publishes a floor no answer can reach, which is how a
// measured seam stays visible without deciding.
type measuredTechClassifier struct {
	verdict *domainGitignore.TechnologyVerdict
	calls   int
}

func (c *measuredTechClassifier) ClassifyTechnologies(context.Context, domainGitignore.ClassifyRequest) (*domainGitignore.TechnologyVerdict, error) {
	c.calls++
	return c.verdict, nil
}

func (c *measuredTechClassifier) TechFloor() float64 { return 1.1 }
