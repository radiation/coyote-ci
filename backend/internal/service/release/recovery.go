package release

import (
	"context"
	"log"
	"time"
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
	s.reconcile(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *RecoveryService) reconcile(ctx context.Context) {
	if err := s.service.ReconcilePending(ctx, 100); err != nil {
		log.Printf("WARNING: application release recovery failed: %v", err)
	}
}
