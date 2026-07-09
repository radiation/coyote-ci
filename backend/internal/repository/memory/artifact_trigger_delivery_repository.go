package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ArtifactTriggerDeliveryRepository struct {
	mu         sync.RWMutex
	deliveries map[string]domain.ArtifactTriggerDelivery
	index      map[string]string
}

func NewArtifactTriggerDeliveryRepository() *ArtifactTriggerDeliveryRepository {
	return &ArtifactTriggerDeliveryRepository{
		deliveries: map[string]domain.ArtifactTriggerDelivery{},
		index:      map[string]string{},
	}
}

func (r *ArtifactTriggerDeliveryRepository) Create(_ context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := artifactTriggerDeliveryKey(delivery.ArtifactID, delivery.ConsumerJobID)
	if _, exists := r.index[key]; exists {
		return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryDuplicate
	}
	if delivery.ID == "" {
		delivery.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = delivery.CreatedAt
	}
	r.deliveries[delivery.ID] = delivery
	r.index[key] = delivery.ID
	return delivery, nil
}

func (r *ArtifactTriggerDeliveryRepository) GetByArtifactIDAndConsumerJobID(_ context.Context, artifactID string, consumerJobID string) (domain.ArtifactTriggerDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.index[artifactTriggerDeliveryKey(artifactID, consumerJobID)]
	if !ok {
		return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryNotFound
	}
	return r.deliveries[id], nil
}

func (r *ArtifactTriggerDeliveryRepository) ListByProducerBuildID(_ context.Context, producerBuildID string) ([]domain.ArtifactTriggerDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trimmedProducerBuildID := producerBuildID
	out := make([]domain.ArtifactTriggerDelivery, 0)
	for _, delivery := range r.deliveries {
		if delivery.ProducerBuildID != trimmedProducerBuildID {
			continue
		}
		out = append(out, delivery)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	return out, nil
}

func (r *ArtifactTriggerDeliveryRepository) Update(_ context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.deliveries[delivery.ID]; !ok {
		return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryNotFound
	}
	r.deliveries[delivery.ID] = delivery
	return delivery, nil
}

func artifactTriggerDeliveryKey(artifactID string, consumerJobID string) string {
	return artifactID + "\x00" + consumerJobID
}
