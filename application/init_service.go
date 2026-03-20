package application

import (
	"context"
	"fmt"
)

type LLMClient interface {
	GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string) ([]string, string, error)
}

type GitReader interface {
	CommitSubjects(ctx context.Context, max int) ([]string, error)
	// CommitLog returns one entry per commit: the subject line followed by the
	// list of changed files, formatted as "subject\n  file1\n  file2".
	CommitLog(ctx context.Context, max int) ([]string, error)
	TopLevelDirs(ctx context.Context) ([]string, error)
	ProjectFiles(ctx context.Context) ([]string, error)
	IsGitRepo(ctx context.Context) bool
}

type InitRequest struct {
	ProjectYMLPath string
	MaxCommits     int
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

	scopeSvc := NewScopeService(s.llm, s.git)

	scopes, err := scopeSvc.Generate(ctx, req.MaxCommits)
	if err != nil {
		return err
	}

	return scopeSvc.MergeAndSave(ctx, req.ProjectYMLPath, scopes)
}
