package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitagenthq/git-agent/infrastructure/config"
)

func writeUserConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestResolve_JevLayerOffWithoutKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")

	cfg, err := config.Resolve(context.Background(), config.ProviderConfig{}, writeUserConfig(t, "model: some-model\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JevAPIKey != "" {
		t.Fatalf("expected the decision layer to be off, got key %q", cfg.JevAPIKey)
	}
}

func TestResolve_JevKeyFromFile(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")

	cfg, err := config.Resolve(context.Background(), config.ProviderConfig{},
		writeUserConfig(t, "jev_api_key: file-key\njev_model: jev-1.13.0\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JevAPIKey != "file-key" || cfg.JevModel != "jev-1.13.0" {
		t.Fatalf("expected the file values, got key=%q model=%q", cfg.JevAPIKey, cfg.JevModel)
	}
}

func TestResolve_JevKeyFromEnvironment(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "env-key")

	cfg, err := config.Resolve(context.Background(), config.ProviderConfig{}, writeUserConfig(t, "model: some-model\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JevAPIKey != "env-key" {
		t.Fatalf("expected the environment key, got %q", cfg.JevAPIKey)
	}
}

func TestResolve_JevKeyFromFlagBeatsEnvironment(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "env-key")

	cfg, err := config.Resolve(context.Background(), config.ProviderConfig{JevAPIKey: "flag-key"},
		writeUserConfig(t, "jev_api_key: file-key\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JevAPIKey != "flag-key" {
		t.Fatalf("expected the flag key to win, got %q", cfg.JevAPIKey)
	}
}

func TestResolve_JevKeyExpandsEnvInFile(t *testing.T) {
	t.Setenv("JEV_TOKEN", "expanded-key")

	cfg, err := config.Resolve(context.Background(), config.ProviderConfig{},
		writeUserConfig(t, "jev_api_key: ${JEV_TOKEN}\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JevAPIKey != "expanded-key" {
		t.Fatalf("expected the env reference to expand, got %q", cfg.JevAPIKey)
	}
}
