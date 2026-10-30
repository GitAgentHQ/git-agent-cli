package cmd

import (
	"testing"

	"github.com/gitagenthq/git-agent/domain/decision"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
)

func TestNewJevLayer_OffWithoutAKey(t *testing.T) {
	// Given a provider config with no TypeSafe key.
	cfg := &infraConfig.ProviderConfig{BaseURL: "https://example.test", Model: "some-model"}

	// When the layer is built.
	layer := newJevLayer(cfg, nil)

	// Then nothing is wired, which keeps every decision with the model.
	if layer != nil {
		t.Fatalf("expected no layer without a key, got %+v", layer)
	}
}

func TestNewJevLayer_OffInTheOffMode(t *testing.T) {
	cfg := &infraConfig.ProviderConfig{JevAPIKey: "key", JevMode: string(decision.ModeOff)}

	if layer := newJevLayer(cfg, nil); layer != nil {
		t.Fatal("expected the off mode to build nothing even with a key")
	}
}

func TestNewJevLayer_ShadowsByDefaultWithAKey(t *testing.T) {
	// Given a key and no mode, which is the state a user reaches by setting only
	// the key.
	cfg := &infraConfig.ProviderConfig{JevAPIKey: "key"}

	layer := newJevLayer(cfg, nil)
	if layer == nil {
		t.Fatal("expected a layer once a key is configured")
	}
	if layer.decisions.Policy.Mode != decision.ModeShadow {
		t.Errorf("expected the shadow default, got %q", layer.decisions.Policy.Mode)
	}
	for name, seam := range map[string]any{
		"scope decider":   layer.ScopeDecider(),
		"tech classifier": layer.TechClassifier(),
		"group decider":   layer.GroupDecider(),
		"type judge":      layer.TypeScopeJudge(),
		"failure router":  layer.FailureRouter(),
	} {
		if seam == nil {
			t.Errorf("expected the %s to be wired in shadow mode", name)
		}
	}
}

func TestNewJevLayer_OnModeActs(t *testing.T) {
	cfg := &infraConfig.ProviderConfig{JevAPIKey: "key", JevMode: "on", JevMinConfidence: 0.8, JevMaxCalls: 2}

	layer := newJevLayer(cfg, nil)
	if layer == nil {
		t.Fatal("expected a layer in the on mode")
	}
	if !layer.decisions.Policy.Adopt(0.85) {
		t.Error("expected the on mode to adopt above the configured floor")
	}
	if layer.decisions.Policy.Adopt(0.7) {
		t.Error("expected the on mode to refuse below the configured floor")
	}
	if layer.decisions.Policy.MaxCalls() != 2 {
		t.Errorf("expected the configured budget, got %d", layer.decisions.Policy.MaxCalls())
	}
}

func TestJevLayer_SeamsTolerateAMissingLayer(t *testing.T) {
	var layer *jevLayer

	if layer.ScopeDecider() != nil || layer.TechClassifier() != nil || layer.GroupDecider() != nil ||
		layer.TypeScopeJudge() != nil || layer.FailureRouter() != nil {
		t.Fatal("expected every seam of a missing layer to be nil")
	}
	// Applying a missing layer must be a no-op rather than a panic.
	layer.Apply(nil, nil)
}

func TestNewJevLayer_ToleratesMissingConfig(t *testing.T) {
	if newJevLayer(nil, nil) != nil {
		t.Fatal("expected no layer for a missing config")
	}
}
