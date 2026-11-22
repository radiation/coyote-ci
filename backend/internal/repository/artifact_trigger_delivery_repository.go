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
var ErrArtifactBuildCommitUnknown = errors.New("artifact build commit outcome unknown")
var ErrArtifactBuildNotCommitted = errors.New("artifact build transaction did not commit")

type ArtifactBuildPlan struct {
	Build             domain.Build
	Steps             []domain.BuildStep
	Jobs              []domain.ExecutionJob
	Outputs           []domain.ExecutionJobOutput
	ConsumerUpdatedAt time.Time
}

func (plan ArtifactBuildPlan) Matches(delivery domain.ArtifactTriggerDelivery, claim time.Time) bool {
	build := plan.Build
	trigger := build.Trigger
	return delivery.Status == domain.ArtifactTriggerDeliveryStatusPending && delivery.QueuedBuildID == nil && delivery.UpdatedAt.Equal(claim) &&
		build.JobID != nil && *build.JobID == delivery.ConsumerJobID && build.ProjectID == delivery.ProducerProjectID && delivery.ConsumerJobID != delivery.ProducerJobID &&
		trigger.Kind == domain.BuildTriggerKindArtifact && trigger.ArtifactID != nil && *trigger.ArtifactID == delivery.ArtifactID &&
		trigger.ProducerBuildID != nil && *trigger.ProducerBuildID == delivery.ProducerBuildID && trigger.ProducerJobID != nil && *trigger.ProducerJobID == delivery.ProducerJobID &&
		trigger.ProducerProjectID != nil && *trigger.ProducerProjectID == delivery.ProducerProjectID && trigger.ArtifactPath != nil && *trigger.ArtifactPath == delivery.ArtifactPath &&
		len(plan.Steps) != 0 && len(plan.Steps) == len(plan.Jobs)
}

type ArtifactTriggerDeliveryRepository interface {
	CreateArtifactBuild(context.Context, string, time.Time, ArtifactBuildPlan) (domain.Build, error)
	FailPending(context.Context, string, time.Time, string) (domain.ArtifactTriggerDelivery, error)
	ClaimFailedForRetry(ctx context.Context, id string, updatedAt time.Time) (domain.ArtifactTriggerDelivery, error)
	Create(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error)
	GetByID(ctx context.Context, id string) (domain.ArtifactTriggerDelivery, error)
	GetByArtifactIDAndConsumerJobID(ctx context.Context, artifactID string, consumerJobID string) (domain.ArtifactTriggerDelivery, error)
	ListByProducerBuildID(ctx context.Context, producerBuildID string) ([]domain.ArtifactTriggerDelivery, error)
}
