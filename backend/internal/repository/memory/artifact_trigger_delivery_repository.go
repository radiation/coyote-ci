package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
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
	consumers  *JobRepository
	builds     *BuildRepository
	jobs       *ExecutionJobRepository
	outputs    *ExecutionJobOutputRepository
}

func NewArtifactBuildDeliveryRepository(consumers *JobRepository, builds *BuildRepository, jobs *ExecutionJobRepository, outputs *ExecutionJobOutputRepository) *ArtifactTriggerDeliveryRepository {
	r := NewArtifactTriggerDeliveryRepository()
	r.consumers, r.builds, r.jobs, r.outputs = consumers, builds, jobs, outputs
	return r
}

func (r *ArtifactTriggerDeliveryRepository) CreateArtifactBuild(_ context.Context, id string, claim time.Time, plan repository.ArtifactBuildPlan) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery, ok := r.deliveries[id]
	if !ok {
		return domain.Build{}, repository.ErrArtifactTriggerDeliveryNotFound
	}
	if r.consumers == nil || r.builds == nil || r.jobs == nil || r.outputs == nil {
		return domain.Build{}, errors.New("artifact build repositories not configured")
	}
	r.consumers.mu.RLock()
	defer r.consumers.mu.RUnlock()
	r.jobs.mu.Lock()
	defer r.jobs.mu.Unlock()
	r.builds.mu.Lock()
	defer r.builds.mu.Unlock()
	r.outputs.mu.Lock()
	defer r.outputs.mu.Unlock()
	if delivery.Status == domain.ArtifactTriggerDeliveryStatusQueued && delivery.QueuedBuildID != nil {
		linked, found := r.builds.builds[*delivery.QueuedBuildID]
		if !found {
			return domain.Build{}, repository.ErrBuildNotFound
		}
		return cloneBuildApplicationVersion(linked), nil
	}
	consumer := r.consumers.jobs[delivery.ConsumerJobID]
	if !plan.Matches(delivery, claim) || !consumer.Enabled || consumer.ProjectID != delivery.ProducerProjectID || !consumer.UpdatedAt.Equal(plan.ConsumerUpdatedAt) {
		return domain.Build{}, repository.ErrArtifactTriggerDeliveryRetryNotClaimable
	}
	created, err := r.builds.createQueuedBuildLocked(plan.Build, plan.Steps)
	if err != nil {
		return domain.Build{}, err
	}
	r.jobs.insertJobsLocked(plan.Jobs)
	r.outputs.insertOutputsLocked(plan.Outputs)
	delivery.QueuedBuildID, delivery.ErrorMessage, delivery.UpdatedAt, delivery.Status = &created.ID, nil, time.Now().UTC(), domain.ArtifactTriggerDeliveryStatusQueued
	r.deliveries[id] = delivery
	return created, nil
}

func (r *ArtifactTriggerDeliveryRepository) FailPending(_ context.Context, id string, claim time.Time, message string) (domain.ArtifactTriggerDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery, ok := r.deliveries[id]
	if !ok {
		return delivery, repository.ErrArtifactTriggerDeliveryNotFound
	}
	if delivery.Status == domain.ArtifactTriggerDeliveryStatusPending && delivery.QueuedBuildID == nil && delivery.UpdatedAt.Equal(claim) {
		delivery.Status, delivery.ErrorMessage, delivery.UpdatedAt = domain.ArtifactTriggerDeliveryStatusFailed, &message, nextDeliveryTimestamp(delivery.UpdatedAt, time.Now().UTC())
		if strings.TrimSpace(message) == "" {
			delivery.ErrorMessage = nil
		}
		r.deliveries[id] = delivery
	}
	return delivery, nil
}

func NewArtifactTriggerDeliveryRepository() *ArtifactTriggerDeliveryRepository {
	return &ArtifactTriggerDeliveryRepository{
		deliveries: map[string]domain.ArtifactTriggerDelivery{},
		index:      map[string]string{},
	}
}

func (r *ArtifactTriggerDeliveryRepository) ClaimFailedForRetry(_ context.Context, id string, updatedAt time.Time) (domain.ArtifactTriggerDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delivery, ok := r.deliveries[strings.TrimSpace(id)]
	if !ok {
		return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryNotFound
	}
	if delivery.Status != domain.ArtifactTriggerDeliveryStatusFailed || delivery.QueuedBuildID != nil {
		return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryRetryNotClaimable
	}
	now := updatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	delivery.Status = domain.ArtifactTriggerDeliveryStatusPending
	delivery.QueuedBuildID = nil
	delivery.ErrorMessage = nil
	delivery.UpdatedAt = nextDeliveryTimestamp(delivery.UpdatedAt, now)
	r.deliveries[delivery.ID] = delivery
	return delivery, nil
}

func nextDeliveryTimestamp(previous, now time.Time) time.Time {
	if !now.After(previous) {
		return previous.Add(time.Microsecond)
	}
	return now
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

	trimmedProducerBuildID := strings.TrimSpace(producerBuildID)
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

func artifactTriggerDeliveryKey(artifactID string, consumerJobID string) string {
	return artifactID + "\x00" + consumerJobID
}

func (r *ArtifactTriggerDeliveryRepository) GetByID(_ context.Context, id string) (domain.ArtifactTriggerDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	delivery, ok := r.deliveries[strings.TrimSpace(id)]
	if !ok {
		return domain.ArtifactTriggerDelivery{}, repository.ErrArtifactTriggerDeliveryNotFound
	}
	return delivery, nil
}
