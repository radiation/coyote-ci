package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrSlackWorkspaceIntegrationNotFound = errors.New("slack workspace integration not found")
var ErrSlackWorkspaceIntegrationConflict = errors.New("slack workspace integration conflict")
var ErrSlackWorkspaceIntegrationReplaceRequired = errors.New("slack workspace integration replacement requires explicit confirmation")

type SlackWorkspaceIntegrationRepository interface {
	Get(ctx context.Context) (domain.SlackWorkspaceIntegration, error)
	ConnectOrReplace(ctx context.Context, candidate domain.SlackWorkspaceIntegration, replaceDifferentWorkspace bool) (domain.SlackWorkspaceIntegration, error)
	SetEnabled(ctx context.Context, enabled bool, updatedAt time.Time) (domain.SlackWorkspaceIntegration, error)
	UpdateLastTestResult(ctx context.Context, testedAt time.Time, succeeded bool) (domain.SlackWorkspaceIntegration, error)
	Delete(ctx context.Context) error
}
