package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrUserNotFound = errors.New("user not found")
var ErrUserEmailConflict = errors.New("user email already exists")
var ErrAPITokenNotFound = errors.New("api token not found")
var ErrProjectMembershipNotFound = errors.New("project membership not found")

type UserRepository interface {
	Create(ctx context.Context, user domain.User) (domain.User, error)
	GetByID(ctx context.Context, id string) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	Update(ctx context.Context, user domain.User) (domain.User, error)
	Delete(ctx context.Context, id string) error
}

type APITokenRepository interface {
	Create(ctx context.Context, token domain.APIToken) (domain.APIToken, error)
	ListByUserID(ctx context.Context, userID string) ([]domain.APIToken, error)
	GetByHash(ctx context.Context, tokenHash string) (domain.APIToken, error)
	RevokeByID(ctx context.Context, userID string, tokenID string, revokedAt time.Time) error
	TouchLastUsed(ctx context.Context, tokenID string, lastUsedAt time.Time) error
}

type ProjectMembershipRepository interface {
	Upsert(ctx context.Context, membership domain.ProjectMembership) (domain.ProjectMembership, error)
	Get(ctx context.Context, projectID string, userID string) (domain.ProjectMembership, error)
	ListByUserID(ctx context.Context, userID string) ([]domain.ProjectMembership, error)
	ListByProjectID(ctx context.Context, projectID string) ([]domain.ProjectMembershipWithUser, error)
	Delete(ctx context.Context, projectID string, userID string) error
}
