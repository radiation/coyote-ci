package release

import (
	"context"
	"log"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type RecoveryService struct {
	service  *Service
	interval time.Duration
}

func NewRecoveryService(service *Service, interval time.Duration) *RecoveryService {
	return &RecoveryService{service: service, interval: interval}
}

func (s *RecoveryService) Run(ctx context.Context) {
	if s == nil || s.service == nil {
		return
	}
	after := s.reconcile(ctx, nil)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			after = s.reconcile(ctx, after)
		}
	}
}

func (s *RecoveryService) reconcile(ctx context.Context, after *repository.PendingReleaseCursor) *repository.PendingReleaseCursor {
	next, err := s.service.reconcilePendingPage(ctx, after, 100)
	if err != nil {
		log.Printf("WARNING: application release recovery failed: %v", err)
	}
	return next
}
