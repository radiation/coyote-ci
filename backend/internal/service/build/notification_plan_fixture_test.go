package build

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type inlineFixturePlans struct {
	*memory.NotificationPlanningRepository
	builds *memory.BuildRepository
}

func (p inlineFixturePlans) CommitNotificationPlan(ctx context.Context, id string, status domain.BuildStatus, deliveries []domain.NotificationDelivery) (bool, error) {
	if _, err := p.builds.Create(ctx, domain.Build{ID: id, Status: status}); err != nil {
		return false, err
	}
	return p.NotificationPlanningRepository.CommitNotificationPlan(ctx, id, status, deliveries)
}

func newTestBuildNotificationService(cfg BuildNotificationConfig) (*BuildNotificationService, error) {
	ledger, ok := cfg.DeliveryRepo.(*memory.NotificationDeliveryRepository)
	if !ok {
		ledger = memory.NewNotificationDeliveryRepository()
	}
	if cfg.DeliveryRepo == nil {
		cfg.DeliveryRepo = ledger
	}
	builds := memory.NewBuildRepository()
	if cfg.PlanningRepo == nil {
		cfg.PlanningRepo = inlineFixturePlans{memory.NewNotificationPlanningRepository(builds, ledger), builds}
	}
	return NewBuildNotificationService(cfg)
}

func newTestNotificationRecoveryDrain(cfg NotificationRecoveryDrainConfig) (*NotificationRecoveryDrain, error) {
	if cfg.Notifier != nil && cfg.Notifier.plans == nil {
		cfg.Notifier.plans = memory.NewNotificationPlanningRepository(memory.NewBuildRepository(), memory.NewNotificationDeliveryRepository())
	}
	return NewNotificationRecoveryDrain(cfg)
}

var _ repository.NotificationPlanningRepository = inlineFixturePlans{}
