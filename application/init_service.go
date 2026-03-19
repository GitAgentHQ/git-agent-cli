package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type LLMClient interface {
	GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string) ([]string, string, error)
}

type GitReader interface {
	CommitSubjects(ctx context.Context, max int) ([]string, error)
	TopLevelDirs(ctx context.Context) ([]string, error)
	ProjectFiles(ctx context.Context) ([]string, error)
	IsGitRepo(ctx context.Context) bool
}

type InitRequest struct {
	ProjectYMLPath string
	HookPath       string
	HookName       string
	Force          bool
	MaxCommits     int
}

var knownHooks = map[string]bool{
	"empty":        true,
	"conventional": true,
}

type InitService struct {
	llm LLMClient
	git GitReader
}

func NewInitService(llm LLMClient, git GitReader) *InitService {
	return &InitService{llm: llm, git: git}
}

func (s *InitService) Init(ctx context.Context, req InitRequest) error {
	if !s.git.IsGitRepo(ctx) {
		return fmt.Errorf("not a git repository")
	}

	if !knownHooks[req.HookName] {
		return fmt.Errorf("unknown hook %q: must be one of empty, conventional, commit-msg", req.HookName)
	}

	if _, err := os.Stat(req.ProjectYMLPath); err == nil && !req.Force {
		return fmt.Errorf("project.yml already exists at %s; use --force to overwrite", req.ProjectYMLPath)
	}

	commits, err := s.git.CommitSubjects(ctx, req.MaxCommits)
	if err != nil {
		return fmt.Errorf("reading commits: %w", err)
	}

	dirs, err := s.git.TopLevelDirs(ctx)
	if err != nil {
		return fmt.Errorf("reading dirs: %w", err)
	}

	files, err := s.git.ProjectFiles(ctx)
	if err != nil {
		return fmt.Errorf("reading project files: %w", err)
	}

	scopes, _, err := s.llm.GenerateScopes(ctx, commits, dirs, files)
	if err != nil {
		return fmt.Errorf("generating scopes: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(req.ProjectYMLPath), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := yaml.Marshal(map[string]any{"scopes": scopes})
	if err != nil {
		return fmt.Errorf("marshalling yaml: %w", err)
	}

	return os.WriteFile(req.ProjectYMLPath, data, 0644)
}
