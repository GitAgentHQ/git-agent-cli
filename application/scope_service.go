package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ScopeService struct {
	llm LLMClient
	git GitReader
}

func NewScopeService(llm LLMClient, git GitReader) *ScopeService {
	return &ScopeService{llm: llm, git: git}
}

func (s *ScopeService) Generate(ctx context.Context, maxCommits int) ([]string, error) {
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

	return scopes, nil
}

func (s *ScopeService) MergeAndSave(ctx context.Context, path string, newScopes []string) error {
	existing := readExistingScopes(path)
	merged := mergeScopes(existing, newScopes)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := yaml.Marshal(map[string]any{"scopes": merged})
	if err != nil {
		return fmt.Errorf("marshalling yaml: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

func readExistingScopes(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Scopes []string `yaml:"scopes"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw.Scopes
}

func mergeScopes(existing, newScopes []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, s := range existing {
		seen[strings.ToLower(s)] = true
	}
	result := make([]string, len(existing))
	copy(result, existing)
	for _, s := range newScopes {
		if !seen[strings.ToLower(s)] {
			result = append(result, s)
			seen[strings.ToLower(s)] = true
		}
	}
	return result
}
