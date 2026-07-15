package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrArtifactTriggerDeliveryNotFound = errors.New("artifact trigger delivery not found")
var ErrArtifactTriggerDeliveryDuplicate = errors.New("artifact trigger delivery already exists")
var ErrArtifactTriggerDeliveryRetryNotClaimable = errors.New("artifact trigger delivery retry not claimable")

type ArtifactTriggerDeliveryRepository interface {
	ClaimFailedForRetry(ctx context.Context, id string, updatedAt time.Time) (domain.ArtifactTriggerDelivery, error)
	Create(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error)
	GetByID(ctx context.Context, id string) (domain.ArtifactTriggerDelivery, error)
	GetByArtifactIDAndConsumerJobID(ctx context.Context, artifactID string, consumerJobID string) (domain.ArtifactTriggerDelivery, error)
	ListByProducerBuildID(ctx context.Context, producerBuildID string) ([]domain.ArtifactTriggerDelivery, error)
	Update(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error)
}
