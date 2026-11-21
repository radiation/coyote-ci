package memory

import (
	"context"
	"errors"
	"sort"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type NotificationPlanningRepository struct {
	builds *BuildRepository
	ledger *NotificationDeliveryRepository
}

func NewNotificationPlanningRepository(builds *BuildRepository, ledger *NotificationDeliveryRepository) *NotificationPlanningRepository {
	return &NotificationPlanningRepository{builds: builds, ledger: ledger}
}

func (r *NotificationPlanningRepository) ListUnplannedBuildIDs(ctx context.Context, after string, limit int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.New("planning batch limit must be positive")
	}
	r.builds.mu.RLock()
	defer r.builds.mu.RUnlock()
	var ids []string
	for id, build := range r.builds.builds {
		if id > after && !r.builds.notificationPlans[id] && (build.Status == domain.BuildStatusFailed || build.Status == domain.BuildStatusSuccess) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids[:min(len(ids), limit)], nil
}

func (r *NotificationPlanningRepository) CommitNotificationPlan(ctx context.Context, id string, status domain.BuildStatus, deliveries []domain.NotificationDelivery) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.builds.mu.Lock()
	defer r.builds.mu.Unlock()
	if r.builds.notificationPlans[id] || r.builds.builds[id].Status != status || (status != domain.BuildStatusSuccess && status != domain.BuildStatusFailed) {
		return false, nil
	}
	pending := NewNotificationDeliveryRepository()
	for _, delivery := range deliveries {
		if err := repository.ValidatePlannedNotification(id, status, delivery); err != nil {
			return false, err
		}
		delivery.ID = ""
		if _, err := pending.createLocked(delivery); err != nil && err != repository.ErrNotificationDeliveryDuplicate {
			return false, err
		}
	}
	r.ledger.mu.Lock()
	defer r.ledger.mu.Unlock()
	for key, deliveryID := range pending.index {
		if _, exists := r.ledger.index[key]; !exists {
			r.ledger.storeCreatedDelivery(key, pending.deliveries[deliveryID])
		}
	}
	r.builds.notificationPlans[id] = true
	return true, nil
}
