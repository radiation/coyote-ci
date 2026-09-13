package service

import (
	"context"
	"io"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

// ImageBuilder is the provider-neutral boundary for durable remote image builds.
type ImageBuilder interface {
	Submit(ctx context.Context, request domain.ImageBuildRequest) (domain.ImageBuildHandle, error)
	Get(ctx context.Context, handle domain.ImageBuildHandle) (domain.ImageBuildResult, error)
	Cancel(ctx context.Context, handle domain.ImageBuildHandle) error
	FindByExecutionJobID(ctx context.Context, executionJobID string) (domain.ImageBuildHandle, bool, error)
}

type ImageBuildSourceStager interface {
	Stage(ctx context.Context, executionJobID string, archive io.Reader) (domain.ImageBuildSource, error)
}
