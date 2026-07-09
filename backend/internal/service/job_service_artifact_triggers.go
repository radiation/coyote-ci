package service

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrJobArtifactTriggerDeliveryRepositoryNotConfigured = errors.New("job artifact trigger delivery repository not configured")

type ArtifactTriggerDeliveryView struct {
	Delivery        domain.ArtifactTriggerDelivery
	ConsumerJobName *string
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
		views = append(views, ArtifactTriggerDeliveryView{
			Delivery:        delivery,
			ConsumerJobName: jobNames[delivery.ConsumerJobID],
		})
	}
	return views, nil
}
