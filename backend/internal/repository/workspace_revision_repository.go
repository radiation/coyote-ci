package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrWorkspaceRevisionNotFound = errors.New("workspace revision not found")
var ErrWorkspaceRevisionConflict = errors.New("workspace revision conflict")
var ErrWorkspaceRevisionStaleClaim = errors.New("workspace revision stale claim")

type WorkspaceRevisionRepository interface {
	CreatePublishing(ctx context.Context, revision domain.WorkspaceRevision) (domain.WorkspaceRevision, error)
	MarkPublishedIfClaimed(ctx context.Context, revisionID string, claimToken string, publication domain.WorkspaceRevisionPublication, publishedAt time.Time) (domain.WorkspaceRevision, error)
	GetByProducingExecutionJob(ctx context.Context, executionJobID string) (domain.WorkspaceRevision, error)
	GetPublishedByBuildNode(ctx context.Context, buildID string, nodeID string) (domain.WorkspaceRevision, error)
	MarkDeleted(ctx context.Context, revisionID string, deletedAt time.Time) (domain.WorkspaceRevision, error)
}
