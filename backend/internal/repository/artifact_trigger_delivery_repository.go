package repository

import (
	"context"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrArtifactTriggerDeliveryNotFound = errors.New("artifact trigger delivery not found")
var ErrArtifactTriggerDeliveryDuplicate = errors.New("artifact trigger delivery already exists")

type ArtifactTriggerDeliveryRepository interface {
	Create(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error)
	GetByArtifactIDAndConsumerJobID(ctx context.Context, artifactID string, consumerJobID string) (domain.ArtifactTriggerDelivery, error)
	Update(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error)
}
