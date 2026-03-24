package openai

import (
	"testing"

	"github.com/gitagenthq/git-agent/domain/project"
)

func TestUnmarshalLLMJSON_WrappedObject(t *testing.T) {
	raw := `{"scopes": [{"name": "app", "description": "application layer"}], "reasoning": "dirs"}`
	var result struct {
		Scopes    []project.Scope `json:"scopes"`
		Reasoning string          `json:"reasoning"`
	}
	if err := unmarshalLLMJSON(raw, "scopes", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 1 || result.Scopes[0].Name != "app" {
		t.Fatalf("got %+v", result.Scopes)
	}
	if result.Reasoning != "dirs" {
		t.Fatalf("reasoning = %q", result.Reasoning)
	}
}

func TestUnmarshalLLMJSON_BareObjectArray(t *testing.T) {
	raw := `[{"name": "app", "description": "application layer"}]`
	var result struct {
		Scopes []project.Scope `json:"scopes"`
	}
	if err := unmarshalLLMJSON(raw, "scopes", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 1 || result.Scopes[0].Name != "app" {
		t.Fatalf("got %+v", result.Scopes)
	}
}

func TestUnmarshalLLMJSON_BareStringArray(t *testing.T) {
	raw := `["app", "infra", "cli"]`
	var result struct {
		Scopes []project.Scope `json:"scopes"`
	}
	if err := unmarshalLLMJSON(raw, "scopes", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 3 {
		t.Fatalf("got %d scopes, want 3", len(result.Scopes))
	}
	if result.Scopes[0].Name != "app" || result.Scopes[2].Name != "cli" {
		t.Fatalf("got %+v", result.Scopes)
	}
}

func TestUnmarshalLLMJSON_BareGroupsArray(t *testing.T) {
	raw := `[{"files": ["a.go"], "title": "fix(app): thing"}]`
	var result struct {
		Groups []struct {
			Files []string `json:"files"`
			Title string   `json:"title"`
		} `json:"groups"`
	}
	if err := unmarshalLLMJSON(raw, "groups", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Groups) != 1 || result.Groups[0].Title != "fix(app): thing" {
		t.Fatalf("got %+v", result.Groups)
	}
}

func TestUnmarshalLLMJSON_BareStringTechnologies(t *testing.T) {
	raw := `["go", "node", "macos"]`
	var result struct {
		Technologies []string `json:"technologies"`
	}
	if err := unmarshalLLMJSON(raw, "technologies", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Technologies) != 3 || result.Technologies[0] != "go" {
		t.Fatalf("got %+v", result.Technologies)
	}
}

func TestUnmarshalLLMJSON_SurroundingText(t *testing.T) {
	raw := "Here is the result:\n```json\n{\"scopes\": [{\"name\": \"app\"}]}\n```"
	var result struct {
		Scopes []project.Scope `json:"scopes"`
	}
	if err := unmarshalLLMJSON(raw, "scopes", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 1 || result.Scopes[0].Name != "app" {
		t.Fatalf("got %+v", result.Scopes)
	}
}

func TestUnmarshalLLMJSON_NoWrapKey(t *testing.T) {
	raw := `{"title": "fix(app): thing", "bullets": ["Do X"], "explanation": "Why."}`
	var result struct {
		Title       string   `json:"title"`
		Bullets     []string `json:"bullets"`
		Explanation string   `json:"explanation"`
	}
	if err := unmarshalLLMJSON(raw, "", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Title != "fix(app): thing" {
		t.Fatalf("title = %q", result.Title)
	}
}

func TestUnmarshalLLMJSON_InvalidJSON(t *testing.T) {
	raw := `not json at all`
	var result struct {
		Scopes []project.Scope `json:"scopes"`
	}
	if err := unmarshalLLMJSON(raw, "scopes", &result); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
