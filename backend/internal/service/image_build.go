package service

import (
	"context"
	"errors"
	"io"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var (
	ErrImmutableImageTagsRequired = errors.New("versioned image publication requires immutable registry tags")
	ErrImagePublicationConflict   = errors.New("versioned image publication conflict")
)

// ImageBuilder is the provider-neutral boundary for durable remote image builds.
type ImageBuilder interface {
	ResolveTargetImageReference(logicalName, applicationVersion string) (string, error)
	Submit(ctx context.Context, request domain.ImageBuildRequest) (domain.ImageBuildHandle, error)
	Get(ctx context.Context, handle domain.ImageBuildHandle) (domain.ImageBuildResult, error)
	Cancel(ctx context.Context, handle domain.ImageBuildHandle) error
	FindByExecutionJobID(ctx context.Context, executionJobID string) (domain.ImageBuildHandle, bool, error)
}

// ImagePublicationInspector verifies the registry properties required by
// versioned image publication and resolves published tag identities.
type ImagePublicationInspector interface {
	EnsureImmutableTags(ctx context.Context, publishedImageReference string) error
	ResolvePublishedDigest(ctx context.Context, publishedImageReference string) (string, bool, error)
}

type ImageBuildSourceStager interface {
	Stage(ctx context.Context, executionJobID string, archive io.Reader, contextPath string, artifacts []ImageBuildContextArtifact) (domain.ImageBuildSource, error)
}

// ImageBuildContextArtifact is a verified artifact stream to materialize in a
// provider's staged Docker context.
type ImageBuildContextArtifact struct {
	Artifact domain.ImageBuildArtifact
	Source   io.ReadCloser
}
