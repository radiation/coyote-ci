package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrSourceCredentialNotFound = errors.New("source credential not found")

type SourceCredentialRepository interface {
	Create(ctx context.Context, credential domain.SourceCredential) (domain.SourceCredential, error)
	List(ctx context.Context) ([]domain.SourceCredential, error)
	GetByID(ctx context.Context, id string) (domain.SourceCredential, error)
	Update(ctx context.Context, credential domain.SourceCredential) (domain.SourceCredential, error)
	Delete(ctx context.Context, id string) error
}
