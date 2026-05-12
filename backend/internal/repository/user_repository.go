package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrUserNotFound = errors.New("user not found")
var ErrUserEmailConflict = errors.New("user email already exists")
var ErrProjectMembershipNotFound = errors.New("project membership not found")

type UserRepository interface {
	Create(ctx context.Context, user domain.User) (domain.User, error)
	GetByID(ctx context.Context, id string) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	Update(ctx context.Context, user domain.User) (domain.User, error)
	Delete(ctx context.Context, id string) error
}

type ProjectMembershipRepository interface {
	Upsert(ctx context.Context, membership domain.ProjectMembership) (domain.ProjectMembership, error)
	Get(ctx context.Context, projectID string, userID string) (domain.ProjectMembership, error)
	ListByUserID(ctx context.Context, userID string) ([]domain.ProjectMembership, error)
	ListByProjectID(ctx context.Context, projectID string) ([]domain.ProjectMembershipWithUser, error)
	Delete(ctx context.Context, projectID string, userID string) error
}
