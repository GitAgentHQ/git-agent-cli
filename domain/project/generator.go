package project

import "context"

// ScopeRequest contains the information needed to generate project scopes.
type ScopeRequest struct {
	RepoPath string
}

// ScopeGenerator generates project scopes from repository structure.
type ScopeGenerator interface {
	Generate(ctx context.Context, req ScopeRequest) (*Config, error)
}
