package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/domain/project"
	"github.com/gitagenthq/git-agent/infrastructure/config"
)

func writeProjectConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git-agent"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git-agent", "config.yml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoadProjectConfig_RequireModelCoAuthor_True(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "require_model_co_author: true\n")

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config when require_model_co_author is set")
	}
	if !cfg.RequireModelCoAuthor {
		t.Errorf("expected RequireModelCoAuthor true, got false")
	}
}

func TestLoadProjectConfig_RequireModelCoAuthor_DefaultFalse(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "scopes:\n  - app\n")

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.RequireModelCoAuthor {
		t.Errorf("expected RequireModelCoAuthor to default to false")
	}
}

func TestLoadProjectConfig_ModelCoAuthorDomains_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "require_model_co_author: true\nmodel_co_author_domains:\n  - acme.ai\n  - example.com\n")

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if got := cfg.ModelCoAuthorDomains; len(got) != 2 || got[0] != "acme.ai" || got[1] != "example.com" {
		t.Errorf("unexpected domains: %v", got)
	}
}

func TestLoadProjectConfig_LocalOverridesProject(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "require_model_co_author: false\n")
	if err := os.WriteFile(
		filepath.Join(dir, ".git-agent", "config.local.yml"),
		[]byte("require_model_co_author: true\nmodel_co_author_domains:\n  - acme.ai\n"),
		0o600,
	); err != nil {
		t.Fatalf("write local: %v", err)
	}

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if !cfg.RequireModelCoAuthor {
		t.Errorf("expected local override to set RequireModelCoAuthor true")
	}
	if len(cfg.ModelCoAuthorDomains) != 1 || cfg.ModelCoAuthorDomains[0] != "acme.ai" {
		t.Errorf("expected local domains override, got: %v", cfg.ModelCoAuthorDomains)
	}
}

func TestLoadProjectConfig_AllZero_ReturnsNil(t *testing.T) {
	// No fields set anywhere → loader still returns nil (existing contract).
	dir := t.TempDir()
	writeProjectConfig(t, dir, "")

	if cfg := config.LoadProjectConfig(dir, ""); cfg != nil {
		t.Errorf("expected nil config for empty file, got: %+v", cfg)
	}
}

func TestKeyRegistry_RequireModelCoAuthor_AllScopes(t *testing.T) {
	def, ok := config.KeyRegistry["require_model_co_author"]
	if !ok {
		t.Fatal("expected require_model_co_author in KeyRegistry")
	}
	if def.Type != "bool" {
		t.Errorf("expected bool type, got %q", def.Type)
	}
	if !def.AllowUser || !def.AllowProject || !def.AllowLocal {
		t.Errorf("expected all three scopes allowed, got user=%v project=%v local=%v",
			def.AllowUser, def.AllowProject, def.AllowLocal)
	}
}

func TestKeyRegistry_ModelCoAuthorDomains_AllScopes(t *testing.T) {
	def, ok := config.KeyRegistry["model_co_author_domains"]
	if !ok {
		t.Fatal("expected model_co_author_domains in KeyRegistry")
	}
	if def.Type != "stringslice" {
		t.Errorf("expected stringslice type, got %q", def.Type)
	}
	if !def.AllowUser || !def.AllowProject || !def.AllowLocal {
		t.Errorf("expected all three scopes allowed, got user=%v project=%v local=%v",
			def.AllowUser, def.AllowProject, def.AllowLocal)
	}
}

func TestLoadProjectConfig_PlanFallback_Heuristic(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "plan_fallback: heuristic\n")

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config when plan_fallback is set")
	}
	if cfg.PlanFallback != "heuristic" {
		t.Errorf("expected PlanFallback %q, got %q", "heuristic", cfg.PlanFallback)
	}
}

func TestLoadProjectConfig_PlanFallback_None(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "plan_fallback: none\n")

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config when plan_fallback is set")
	}
	if cfg.PlanFallback != "none" {
		t.Errorf("expected PlanFallback %q, got %q", "none", cfg.PlanFallback)
	}
}

func TestLoadProjectConfig_PlanFallback_AbsentIsZero(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "scopes:\n  - app\n")

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.PlanFallback != "" {
		t.Errorf("expected empty PlanFallback when absent, got %q", cfg.PlanFallback)
	}
}

func TestLoadProjectConfig_PlanFallback_LocalOverridesProject(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir, "plan_fallback: none\n")
	if err := os.WriteFile(
		filepath.Join(dir, ".git-agent", "config.local.yml"),
		[]byte("plan_fallback: heuristic\n"),
		0o600,
	); err != nil {
		t.Fatalf("write local: %v", err)
	}

	cfg := config.LoadProjectConfig(dir, "")
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.PlanFallback != "heuristic" {
		t.Errorf("expected local override 'heuristic', got %q", cfg.PlanFallback)
	}
}

func TestProjectConfig_PlanFallbackConstants(t *testing.T) {
	if project.PlanFallbackNone != "none" {
		t.Errorf("expected PlanFallbackNone %q, got %q", "none", project.PlanFallbackNone)
	}
	if project.PlanFallbackHeuristic != "heuristic" {
		t.Errorf("expected PlanFallbackHeuristic %q, got %q", "heuristic", project.PlanFallbackHeuristic)
	}
}

func TestResolveKey_KebabAliases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"require-model-co-author", "require_model_co_author"},
		{"model-co-author-domains", "model_co_author_domains"},
	}
	for _, tc := range cases {
		got, err := config.ResolveKey(tc.in)
		if err != nil {
			t.Errorf("ResolveKey(%q) failed: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ResolveKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
