package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrUserSlackIdentityNotFound = errors.New("user slack identity not found")
var ErrUserSlackIdentityConflict = errors.New("user slack identity already exists")

type UserSlackIdentityRepository interface {
	GetByUserID(ctx context.Context, userID string) (domain.UserSlackIdentity, error)
	Upsert(ctx context.Context, identity domain.UserSlackIdentity) (domain.UserSlackIdentity, error)
	SetEnabled(ctx context.Context, userID string, enabled bool, updatedAt time.Time) (domain.UserSlackIdentity, error)
	DeleteByUserID(ctx context.Context, userID string) error
	CountByWorkspaceIntegrationID(ctx context.Context, workspaceIntegrationID string) (int, error)
}
