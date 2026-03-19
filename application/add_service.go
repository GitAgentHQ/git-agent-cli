package application

import "context"

type GitClient interface {
	Add(ctx context.Context, paths []string) error
}

type AddService struct {
	git GitClient
}

func NewAddService(git GitClient) *AddService {
	return &AddService{git: git}
}

func (s *AddService) Add(ctx context.Context, paths []string) error {
	return s.git.Add(ctx, paths)
}
