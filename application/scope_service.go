package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gitagenthq/git-agent/domain/project"
)

type ScopeService struct {
	llm LLMClient
	git GitReader
}

func NewScopeService(llm LLMClient, git GitReader) *ScopeService {
	return &ScopeService{llm: llm, git: git}
}

func (s *ScopeService) Generate(ctx context.Context, maxCommits int) ([]project.Scope, error) {
	commits, err := s.git.CommitLog(ctx, maxCommits)
	if err != nil {
		return nil, fmt.Errorf("reading commit log: %w", err)
	}

	dirs, err := s.git.TopLevelDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading dirs: %w", err)
	}

	files, err := s.git.ProjectFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading project files: %w", err)
	}

	scopes, _, err := s.llm.GenerateScopes(ctx, commits, dirs, files)
	if err != nil {
		return nil, fmt.Errorf("generating scopes: %w", err)
	}

	return filterConventionalTypes(scopes), nil
}

// conventionalTypes is the standard set of conventional commit types.
// Scopes must not duplicate these names.
var conventionalTypes = map[string]bool{
	"build": true, "ci": true, "docs": true, "feat": true, "fix": true,
	"perf": true, "refactor": true, "style": true, "test": true,
	"chore": true, "revert": true,
}

func filterConventionalTypes(scopes []project.Scope) []project.Scope {
	result := scopes[:0:0]
	for _, s := range scopes {
		if !conventionalTypes[strings.ToLower(s.Name)] {
			result = append(result, s)
		}
	}
	return result
}

func (s *ScopeService) MergeAndSave(ctx context.Context, path string, newScopes []project.Scope) error {
	// Read full YAML map to preserve all existing keys (e.g., hook).
	rawMap := readExistingYAMLMap(path)

	var existingScopes []project.Scope
	if v, ok := rawMap["scopes"]; ok {
		existingScopes = parseScopesFromYAML(v)
	}

	rawMap["scopes"] = mergeScopes(existingScopes, newScopes)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := yaml.Marshal(rawMap)
	if err != nil {
		return fmt.Errorf("marshalling yaml: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// parseScopesFromYAML handles both legacy string format and new structured format.
func parseScopesFromYAML(v any) []project.Scope {
	switch sv := v.(type) {
	case []interface{}:
		var scopes []project.Scope
		for _, item := range sv {
			switch val := item.(type) {
			case string:
				scopes = append(scopes, project.Scope{Name: val})
			case map[string]interface{}:
				s := project.Scope{}
				if name, ok := val["name"].(string); ok {
					s.Name = name
				}
				if desc, ok := val["description"].(string); ok {
					s.Description = desc
				}
				if s.Name != "" {
					scopes = append(scopes, s)
				}
			}
		}
		return scopes
	}
	return nil
}

func readExistingYAMLMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]any)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil || m == nil {
		return make(map[string]any)
	}
	return m
}

func mergeScopes(existing, newScopes []project.Scope) []project.Scope {
	seen := make(map[string]int, len(existing))
	for i, s := range existing {
		seen[strings.ToLower(s.Name)] = i
	}
	result := make([]project.Scope, len(existing))
	copy(result, existing)
	for _, s := range newScopes {
		key := strings.ToLower(s.Name)
		if idx, ok := seen[key]; ok {
			// Update description if the existing one is empty and the new one has one.
			if result[idx].Description == "" && s.Description != "" {
				result[idx].Description = s.Description
			}
		} else {
			result = append(result, s)
			seen[key] = len(result) - 1
		}
	}
	return result
}
