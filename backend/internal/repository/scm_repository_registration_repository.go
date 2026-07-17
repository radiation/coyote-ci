package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrSCMRepositoryRegistrationNotFound = errors.New("scm registered repository not found")
var ErrSCMRepositoryRegistrationDuplicate = errors.New("scm registered repository already exists for this connection and provider repository id")

type SCMRepositoryRegistrationRepository interface {
	Create(ctx context.Context, registration domain.SCMRepositoryRegistration) (domain.SCMRepositoryRegistration, error)
	List(ctx context.Context) ([]domain.SCMRepositoryRegistration, error)
	GetByID(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error)
	Update(ctx context.Context, registration domain.SCMRepositoryRegistration) (domain.SCMRepositoryRegistration, error)
}
