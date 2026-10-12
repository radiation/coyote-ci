package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var (
	ErrApplicationReleaseNotFound     = errors.New("application release not found")
	ErrApplicationReleaseConflict     = errors.New("application release version already exists")
	ErrApplicationReleaseIncomplete   = errors.New("application release is not complete")
	ErrApplicationReleaseChannelScope = errors.New("application release channels require a job-backed release")
)

type ApplicationReleaseRepository interface {
	Create(ctx context.Context, release domain.ApplicationRelease) (domain.ApplicationRelease, error)
	GetByBuildID(ctx context.Context, buildID string) (domain.ApplicationRelease, error)
	GetByID(ctx context.Context, releaseID string) (domain.ApplicationRelease, error)
	ListPending(ctx context.Context, limit int) ([]domain.ApplicationRelease, error)
	Reconcile(ctx context.Context, releaseID string) (domain.ApplicationRelease, error)
	FailBuildRelease(ctx context.Context, buildID string, reason string) (domain.ApplicationRelease, error)
	PromoteStable(ctx context.Context, releaseID string, actorUserID *string) (domain.ApplicationRelease, error)
}
