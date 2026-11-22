package service

import (
	"context"
	"errors"
	"log"
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

	updated, err := s.processArtifactDelivery(ctx, claimed)
	if err != nil {
		return RetryArtifactTriggerDeliveryResult{}, err
	}
	view, err := s.artifactTriggerDeliveryView(ctx, updated)
	if err != nil {
		return RetryArtifactTriggerDeliveryResult{}, err
	}
	return RetryArtifactTriggerDeliveryResult{Result: "retried", Message: "queued downstream build", View: view}, nil
}

func (s *JobService) processArtifactDelivery(ctx context.Context, claimed domain.ArtifactTriggerDelivery) (domain.ArtifactTriggerDelivery, error) {
	current, err := s.artifactTriggerDeliveries.GetByID(ctx, claimed.ID)
	if err != nil || current.QueuedBuildID != nil {
		return current, err
	}
	if current.Status != domain.ArtifactTriggerDeliveryStatusPending || !current.UpdatedAt.Equal(claimed.UpdatedAt) {
		return current, repository.ErrArtifactTriggerDeliveryRetryNotClaimable
	}
	producerBuild, err := s.buildService.GetBuild(ctx, claimed.ProducerBuildID)
	if err != nil {
		return current, err
	}
	if !domain.IsTerminalBuildStatus(producerBuild.Status) {
		return current, ErrArtifactTriggerDeliveryPendingRetryDeferred
	}
	consumerJob, err := s.jobRepo.GetByID(ctx, claimed.ConsumerJobID)
	if err != nil {
		s.failArtifactTriggerDeliveryRetry(ctx, claimed, err)
		return current, err
	}
	artifacts, err := s.buildService.GetBuildArtifacts(ctx, claimed.ProducerBuildID)
	if err != nil {
		s.failArtifactTriggerDeliveryRetry(ctx, claimed, err)
		return current, err
	}
	artifact, ok := artifactTriggerArtifactByID(artifacts, claimed.ArtifactID)
	if !ok {
		s.failArtifactTriggerDeliveryRetry(ctx, claimed, buildsvc.ErrArtifactNotFound)
		return current, buildsvc.ErrArtifactNotFound
	}
	return s.queueArtifactTriggerDelivery(ctx, claimed, producerBuild, artifact, consumerJob)
}

func (s *JobService) recoverArtifactPage(ctx context.Context, after string, limit int) (string, error) {
	page, err := s.artifactTriggerDeliveries.ListPendingAfter(ctx, after, limit)
	if err != nil {
		return after, err
	}
	for _, delivery := range page {
		if _, processErr := s.processArtifactDelivery(ctx, delivery); processErr != nil {
			log.Printf("artifact trigger recovery: delivery_id=%s err=%v", delivery.ID, processErr)
		}
	}
	if len(page) == limit {
		return page[len(page)-1].ID, nil
	}
	return "", nil
}

func (s *JobService) RunArtifactTriggerRecovery(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var after string
	for ctx.Err() == nil {
		var err error
		after, err = s.recoverArtifactPage(ctx, after, 64)
		if err != nil {
			log.Printf("artifact trigger discovery: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *JobService) failArtifactTriggerDeliveryRetry(ctx context.Context, delivery domain.ArtifactTriggerDelivery, retryErr error) {
	if s.artifactTriggerDeliveries == nil || retryErr == nil {
		return
	}
	_, _ = s.artifactTriggerDeliveries.FailPending(ctx, delivery.ID, delivery.UpdatedAt, retryErr.Error())
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
