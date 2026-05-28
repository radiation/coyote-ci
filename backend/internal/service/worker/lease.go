package worker

import (
	"context"
	"errors"
	"hash/fnv"
	"log"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func (w *ExecutionWorkerService) heartbeatInterval() time.Duration {
	interval := w.leaseDuration / 3
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func (w *ExecutionWorkerService) heartbeatIntervalForStep(step WorkerRunnableStep) time.Duration {
	base := w.heartbeatInterval()
	window := base / 5
	if window <= 0 {
		return base
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(step.WorkerID))
	_, _ = h.Write([]byte(step.ClaimToken))

	spread := int64((2 * window) + 1)
	offset := time.Duration(int64(h.Sum32())%spread - int64(window))
	interval := base + offset

	minInterval := 100 * time.Millisecond
	if interval < minInterval {
		interval = minInterval
	}

	maxInterval := w.leaseDuration - (w.leaseDuration / 10)
	if maxInterval > minInterval && interval > maxInterval {
		interval = maxInterval
	}

	return interval
}

func (w *ExecutionWorkerService) RecoveryStats() WorkerLeaseRecoveryStats {
	return WorkerLeaseRecoveryStats{
		ClaimsWon:     atomic.LoadInt64(&w.claimsWon),
		ReclaimsWon:   atomic.LoadInt64(&w.reclaimsWon),
		RenewalsWon:   atomic.LoadInt64(&w.renewalsWon),
		RenewalsStale: atomic.LoadInt64(&w.renewalsStale),
		StaleComplete: atomic.LoadInt64(&w.staleComplete),
		ReclaimMisses: atomic.LoadInt64(&w.reclaimMisses),
	}
}

func (w *ExecutionWorkerService) renewStepLease(ctx context.Context, step WorkerRunnableStep) (bool, error) {
	leaseExpiresAt := w.clock().UTC().Add(w.leaseDuration)
	if step.JobID != "" {
		_, renewed, renewErr := w.builds.RenewJobLease(ctx, step.JobID, step.ClaimToken, leaseExpiresAt)
		if renewErr != nil {
			if errors.Is(renewErr, buildsvc.ErrStaleStepClaim) {
				staleCount := atomic.AddInt64(&w.renewalsStale, 1)
				log.Printf("job lease renewal rejected as stale: job_id=%s build_id=%s step=%s stale_count=%d", step.JobID, step.BuildID, step.StepName, staleCount)
				return false, nil
			}
			return false, renewErr
		}
		if !renewed {
			staleCount := atomic.AddInt64(&w.renewalsStale, 1)
			log.Printf("job lease renewal rejected: job_id=%s build_id=%s step=%s stale_count=%d", step.JobID, step.BuildID, step.StepName, staleCount)
			return false, nil
		}
	}

	_, renewedStep, stepErr := w.builds.RenewStepLease(ctx, step.BuildID, step.StepIndex, step.ClaimToken, leaseExpiresAt)
	if stepErr != nil {
		if errors.Is(stepErr, buildsvc.ErrStaleStepClaim) {
			staleCount := atomic.AddInt64(&w.renewalsStale, 1)
			log.Printf("step lease renewal rejected as stale: build_id=%s step=%s stale_count=%d", step.BuildID, step.StepName, staleCount)
			return false, nil
		}
		return false, stepErr
	}
	if !renewedStep {
		staleCount := atomic.AddInt64(&w.renewalsStale, 1)
		log.Printf("step lease renewal rejected: build_id=%s step=%s stale_count=%d", step.BuildID, step.StepName, staleCount)
		return false, nil
	}

	renewCount := atomic.AddInt64(&w.renewalsWon, 1)
	log.Printf("lease renewal succeeded: build_id=%s step=%s renewal_count=%d", step.BuildID, step.StepName, renewCount)

	return true, nil
}

func (w *ExecutionWorkerService) newStepClaim() repository.StepClaim {
	now := w.clock().UTC()
	return repository.StepClaim{
		WorkerID:       w.workerID,
		ClaimToken:     uuid.NewString(),
		ClaimedAt:      now,
		LeaseExpiresAt: now.Add(w.leaseDuration),
	}
}

func (w *ExecutionWorkerService) recordHeartbeat(ctx context.Context, force bool) {
	if w.workerRepo == nil {
		return
	}

	now := w.clock().UTC()
	if !w.reserveHeartbeatWrite(now, force) {
		return
	}

	_, err := w.workerRepo.UpsertHeartbeat(ctx, domain.WorkerHeartbeat{
		ID:          w.workerID,
		Name:        w.workerID,
		HeartbeatAt: now,
	})
	if err != nil {
		log.Printf("worker heartbeat update failed: worker_id=%s err=%v", w.workerID, err)
	}
}

func (w *ExecutionWorkerService) reserveHeartbeatWrite(now time.Time, force bool) bool {
	nowUnix := now.UnixNano()
	if force || w.heartbeatWriteInterval <= 0 {
		atomic.StoreInt64(&w.lastHeartbeatWriteAt, nowUnix)
		return true
	}

	interval := int64(w.heartbeatWriteInterval)
	for {
		last := atomic.LoadInt64(&w.lastHeartbeatWriteAt)
		if last != 0 && nowUnix-last < interval {
			return false
		}
		if atomic.CompareAndSwapInt64(&w.lastHeartbeatWriteAt, last, nowUnix) {
			return true
		}
	}
}
