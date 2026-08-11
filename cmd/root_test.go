package cmd_test

import (
	"testing"

	"github.com/gitagenthq/git-agent/cmd"
	"github.com/gitagenthq/git-agent/domain/project"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
)

// TestProviderConfigError_FreeGateway allows zero-config free mode: an empty
// api_key + a base_url (the built-in shared gateway) passes without a model,
// because the Worker pins the model server-side.
func TestProviderConfigError_FreeGateway(t *testing.T) {
	cfg := &infraConfig.ProviderConfig{BaseURL: "https://git-agent-gateway.example.com"}
	if got := cmd.ProviderConfigErrorForTest(cfg); got != "" {
		t.Errorf("expected free-gateway config to pass, got error: %q", got)
	}
}

// TestProviderConfigError_NoProvider reports an actionable error when nothing
// is configured (dev build with no built-in gateway URL).
func TestProviderConfigError_NoProvider(t *testing.T) {
	cfg := &infraConfig.ProviderConfig{}
	if got := cmd.ProviderConfigErrorForTest(cfg); got == "" {
		t.Fatal("expected error when no provider is configured, got none")
	}
}

// TestProviderConfigError_NilConfig exercises the nil branch: a nil config
// must not panic and must return an error.
func TestProviderConfigError_NilConfig(t *testing.T) {
	if got := cmd.ProviderConfigErrorForTest(nil); got == "" {
		t.Fatal("expected error for nil config, got none")
	}
}

// TestProviderConfigError_DirectRequiresBaseURLAndModel: when a user supplies
// an api_key (direct mode), base_url and model must both be present.
func TestProviderConfigError_DirectRequiresBaseURLAndModel(t *testing.T) {
	// api_key set, but no base_url/model → incomplete.
	incomplete := &infraConfig.ProviderConfig{APIKey: "sk-test"}
	if got := cmd.ProviderConfigErrorForTest(incomplete); got == "" {
		t.Fatal("expected incomplete-config error when api_key set but base_url/model missing")
	}
	// api_key + base_url + model → valid direct mode.
	complete := &infraConfig.ProviderConfig{
		APIKey:  "sk-test",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o",
	}
	if got := cmd.ProviderConfigErrorForTest(complete); got != "" {
		t.Errorf("expected complete direct config to pass, got error: %q", got)
	}
}

func TestHasUncoveredDirs(t *testing.T) {
	existingScopes := []project.Scope{
		{Name: "git", Description: "git package in git/"},
		{Name: "pi", Description: "pi agent package in pi/"},
	}

	// 1. Files in git-agent directory should be detected as UNCOVERED when scope is git
	files := []string{"git-agent/package.json", "git-agent/skills/commit/SKILL.md"}
	if !cmd.HasUncoveredDirsForTest(files, existingScopes) {
		t.Errorf("expected git-agent/ files to be detected as uncovered when scopes only contain git and pi")
	}

	// 2. Files in git/ directory should be detected as COVERED
	gitFiles := []string{"git/skills/commit/SKILL.md"}
	if cmd.HasUncoveredDirsForTest(gitFiles, existingScopes) {
		t.Errorf("expected git/ files to be covered by existing git scope")
	}

	// 3. Files in directory with matching scope description should be COVERED
	scopesWithGA := append(existingScopes, project.Scope{Name: "ga", Description: "git-agent/ plugin"})
	if cmd.HasUncoveredDirsForTest(files, scopesWithGA) {
		t.Errorf("expected git-agent/ files to be covered when scope description references git-agent/")
	}
}
