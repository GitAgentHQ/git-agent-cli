package application

import (
	"context"
	"fmt"

	"github.com/gitagenthq/git-agent/domain/project"
)

type LLMClient interface {
	GenerateScopes(ctx context.Context, commits []string, dirs []string, files []string, existingScopes []project.Scope) ([]project.Scope, string, error)
	// DescribeScopes writes one description per proposed scope. The model only
	// chooses the wording of scopes that another layer already selected, so it
	// must return the proposed names unchanged.
	DescribeScopes(ctx context.Context, proposed []project.ProposedScope) ([]project.Scope, error)
}

type GitReader interface {
	CommitSubjects(ctx context.Context, max int) ([]string, error)
	// CommitLog returns one entry per commit: the subject line followed by the
	// list of changed files, formatted as "subject\n  file1\n  file2".
	CommitLog(ctx context.Context, max int) ([]string, error)
	TopLevelDirs(ctx context.Context) ([]string, error)
	ProjectFiles(ctx context.Context) ([]string, error)
	IsGitRepo(ctx context.Context) bool
	RepoRoot(ctx context.Context) (string, error)
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

	// The CLI wires the optional scope decision layer through NewScopeService,
	// so InitService runs the plain model path.
	scopeSvc := NewScopeService(s.llm, s.git, nil, nil)

	existingScopes := ReadScopes(req.ProjectYMLPath)
	scopes, err := scopeSvc.Generate(ctx, req.MaxCommits, existingScopes)
	if err != nil {
		return err
	}

	_, err = scopeSvc.MergeAndSave(ctx, req.ProjectYMLPath, scopes)
	return err
}
