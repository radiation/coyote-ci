package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

var ErrJobArtifactTriggerDeliveryRepositoryNotConfigured = errors.New("job artifact trigger delivery repository not configured")

type ArtifactTriggerDeliveryView struct {
	Delivery        domain.ArtifactTriggerDelivery
	ConsumerJobName *string
}

type RetryArtifactTriggerDeliveryResult struct {
	Result  string
	Message string
	View    ArtifactTriggerDeliveryView
}

func (s *JobService) GetArtifactTriggerDeliveryByID(ctx context.Context, deliveryID string) (ArtifactTriggerDeliveryView, error) {
	if s.artifactTriggerDeliveries == nil {
		return ArtifactTriggerDeliveryView{}, ErrJobArtifactTriggerDeliveryRepositoryNotConfigured
	}

	delivery, err := s.artifactTriggerDeliveries.GetByID(ctx, strings.TrimSpace(deliveryID))
	if err != nil {
		return ArtifactTriggerDeliveryView{}, err
	}
	return s.artifactTriggerDeliveryView(ctx, delivery)
}

func (s *JobService) ListArtifactTriggerDeliveriesByProducerBuildID(ctx context.Context, producerBuildID string) ([]ArtifactTriggerDeliveryView, error) {
	if s.artifactTriggerDeliveries == nil {
		return nil, ErrJobArtifactTriggerDeliveryRepositoryNotConfigured
	}

	deliveries, err := s.artifactTriggerDeliveries.ListByProducerBuildID(ctx, strings.TrimSpace(producerBuildID))
	if err != nil {
		return nil, err
	}
	if len(deliveries) == 0 {
		return []ArtifactTriggerDeliveryView{}, nil
	}

	jobNames := map[string]*string{}
	if s.jobRepo != nil {
		jobIDs := make([]string, 0, len(deliveries))
		for _, delivery := range deliveries {
			jobIDs = append(jobIDs, delivery.ConsumerJobID)
		}
		jobs, jobErr := s.jobRepo.GetByIDs(ctx, jobIDs)
		if jobErr != nil {
			return nil, jobErr
		}
		for _, job := range jobs {
			name := strings.TrimSpace(job.Name)
			if name == "" {
				continue
			}
			value := name
			jobNames[job.ID] = &value
		}
	}

	views := make([]ArtifactTriggerDeliveryView, 0, len(deliveries))
	for _, delivery := range deliveries {
		views = append(views, ArtifactTriggerDeliveryView{Delivery: delivery, ConsumerJobName: jobNames[delivery.ConsumerJobID]})
	}
	return views, nil
}

func (s *JobService) RetryArtifactTriggerDelivery(ctx context.Context, deliveryID string) (RetryArtifactTriggerDeliveryResult, error) {
	if s.artifactTriggerDeliveries == nil {
		return RetryArtifactTriggerDeliveryResult{}, ErrJobArtifactTriggerDeliveryRepositoryNotConfigured
	}
	if s.buildService == nil {
		return RetryArtifactTriggerDeliveryResult{}, ErrJobBuildServiceNotConfigured
	}
	if s.jobRepo == nil {
		return RetryArtifactTriggerDeliveryResult{}, ErrJobNotFound
	}

	trimmedDeliveryID := strings.TrimSpace(deliveryID)
	claimed, err := s.artifactTriggerDeliveries.ClaimFailedForRetry(ctx, trimmedDeliveryID, time.Now().UTC())
	if err != nil {
		if errors.Is(err, repository.ErrArtifactTriggerDeliveryRetryNotClaimable) {
			current, getErr := s.GetArtifactTriggerDeliveryByID(ctx, trimmedDeliveryID)
			if getErr != nil {
				return RetryArtifactTriggerDeliveryResult{}, getErr
			}
			delivery := current.Delivery
			if delivery.Status == domain.ArtifactTriggerDeliveryStatusQueued && delivery.QueuedBuildID != nil {
				return RetryArtifactTriggerDeliveryResult{
					Result:  "already_satisfied",
					Message: "artifact trigger delivery already points at a downstream build",
					View:    current,
				}, nil
			}
			if delivery.QueuedBuildID != nil {
				return RetryArtifactTriggerDeliveryResult{}, ErrArtifactTriggerDeliveryQueuedBuildConflict
			}
			if delivery.Status == domain.ArtifactTriggerDeliveryStatusPending {
				return RetryArtifactTriggerDeliveryResult{}, ErrArtifactTriggerDeliveryPendingRetryDeferred
			}
			return RetryArtifactTriggerDeliveryResult{}, ErrArtifactTriggerDeliveryRetryNotSupported
		}
		return RetryArtifactTriggerDeliveryResult{}, err
	}

	consumerJob, err := s.jobRepo.GetByID(ctx, claimed.ConsumerJobID)
	if err != nil {
		s.failArtifactTriggerDeliveryRetry(ctx, claimed, err)
		return RetryArtifactTriggerDeliveryResult{}, err
	}
	artifacts, err := s.buildService.GetBuildArtifacts(ctx, claimed.ProducerBuildID)
	if err != nil {
		s.failArtifactTriggerDeliveryRetry(ctx, claimed, err)
		return RetryArtifactTriggerDeliveryResult{}, err
	}
	artifact, ok := artifactTriggerArtifactByID(artifacts, claimed.ArtifactID)
	if !ok {
		s.failArtifactTriggerDeliveryRetry(ctx, claimed, buildsvc.ErrArtifactNotFound)
		return RetryArtifactTriggerDeliveryResult{}, buildsvc.ErrArtifactNotFound
	}
	producerJobID := strings.TrimSpace(claimed.ProducerJobID)
	producerBuild := domain.Build{ID: claimed.ProducerBuildID, ProjectID: claimed.ProducerProjectID, JobID: &producerJobID}
	updated, queueErr := s.queueArtifactTriggerDelivery(ctx, claimed, producerBuild, artifact, consumerJob)
	if queueErr != nil {
		return RetryArtifactTriggerDeliveryResult{}, queueErr
	}
	view, err := s.artifactTriggerDeliveryView(ctx, updated)
	if err != nil {
		return RetryArtifactTriggerDeliveryResult{}, err
	}
	return RetryArtifactTriggerDeliveryResult{Result: "retried", Message: "queued downstream build", View: view}, nil
}

func (s *JobService) failArtifactTriggerDeliveryRetry(ctx context.Context, delivery domain.ArtifactTriggerDelivery, retryErr error) {
	if s.artifactTriggerDeliveries == nil || retryErr == nil {
		return
	}
	message := strings.TrimSpace(retryErr.Error())
	delivery.Status = domain.ArtifactTriggerDeliveryStatusFailed
	delivery.QueuedBuildID = nil
	delivery.UpdatedAt = time.Now().UTC()
	if message == "" {
		delivery.ErrorMessage = nil
	} else {
		delivery.ErrorMessage = &message
	}
	_, _ = s.artifactTriggerDeliveries.Update(ctx, delivery)
}

func (s *JobService) artifactTriggerDeliveryView(ctx context.Context, delivery domain.ArtifactTriggerDelivery) (ArtifactTriggerDeliveryView, error) {
	view := ArtifactTriggerDeliveryView{Delivery: delivery}
	if s.jobRepo == nil || strings.TrimSpace(delivery.ConsumerJobID) == "" {
		return view, nil
	}
	job, err := s.jobRepo.GetByID(ctx, delivery.ConsumerJobID)
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return view, nil
		}
		return ArtifactTriggerDeliveryView{}, err
	}
	if name := strings.TrimSpace(job.Name); name != "" {
		view.ConsumerJobName = &name
	}
	return view, nil
}

func artifactTriggerArtifactByID(artifacts []domain.BuildArtifact, artifactID string) (domain.BuildArtifact, bool) {
	trimmedArtifactID := strings.TrimSpace(artifactID)
	for _, artifact := range artifacts {
		if artifact.ID == trimmedArtifactID {
			return artifact, true
		}
	}
	return domain.BuildArtifact{}, false
}
