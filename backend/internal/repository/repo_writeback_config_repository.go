package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrRepoWritebackConfigNotFound = errors.New("repo writeback config not found")

type RepoWritebackConfigRepository interface {
	Create(ctx context.Context, config domain.RepoWritebackConfig) (domain.RepoWritebackConfig, error)
	ListByProjectID(ctx context.Context, projectID string) ([]domain.RepoWritebackConfig, error)
	GetByID(ctx context.Context, id string) (domain.RepoWritebackConfig, error)
	GetByProjectAndRepo(ctx context.Context, projectID string, repositoryURL string) (domain.RepoWritebackConfig, error)
	Update(ctx context.Context, config domain.RepoWritebackConfig) (domain.RepoWritebackConfig, error)
	Delete(ctx context.Context, id string) error
}
