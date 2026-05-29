package worker

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type workerExecutionBoundary interface {
	ClaimNextRunnableJob(ctx context.Context, claim repository.StepClaim) (domain.ExecutionJob, bool, error)
	PrepareBuildExecution(ctx context.Context, id string) (domain.Build, error)
	GetBuild(ctx context.Context, id string) (domain.Build, error)
	ListBuilds(ctx context.Context) ([]domain.Build, error)
	GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error)
	GetJobByStepID(ctx context.Context, stepID string) (domain.ExecutionJob, error)
	ClaimJobByStepID(ctx context.Context, stepID string, claim repository.StepClaim) (domain.ExecutionJob, bool, error)
	RenewJobLease(ctx context.Context, jobID string, claimToken string, leaseExpiresAt time.Time) (domain.ExecutionJob, bool, error)
	ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error)
	ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error)
	RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, bool, error)
	QueueBuild(ctx context.Context, id string) (domain.Build, error)
	StartBuild(ctx context.Context, id string) (domain.Build, error)
	CompleteBuild(ctx context.Context, id string) (domain.Build, error)
	FailBuild(ctx context.Context, id string) (domain.Build, error)
	RunStep(ctx context.Context, request runner.RunStepRequest) (runner.RunStepResult, buildsvc.StepCompletionReport, error)
}

type WorkerRunnableStep struct {
	BuildID        string
	JobID          string
	StepID         string
	StepIndex      int
	StepName       string
	WorkerID       string
	ClaimToken     string
	Image          string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
}

type WorkerStepExecutionReport struct {
	BuildID         string
	Step            domain.BuildStep
	Result          runner.RunStepResult
	SideEffectError *string
}

type WorkerLeaseRecoveryStats struct {
	ClaimsWon     int64 `json:"claims_won"`
	ReclaimsWon   int64 `json:"reclaims_won"`
	RenewalsWon   int64 `json:"renewals_won"`
	RenewalsStale int64 `json:"renewals_stale"`
	StaleComplete int64 `json:"stale_completion_rejected"`
	ReclaimMisses int64 `json:"reclaim_misses"`
}

type ExecutionWorkerService struct {
	builds                 workerExecutionBoundary
	workerRepo             repository.WorkerRepository
	workerID               string
	leaseDuration          time.Duration
	heartbeatWriteInterval time.Duration
	clock                  func() time.Time
	lastHeartbeatWriteAt   int64
	claimsWon              int64
	reclaimsWon            int64
	renewalsWon            int64
	renewalsStale          int64
	staleComplete          int64
	reclaimMisses          int64
}

func NewExecutionWorkerService(builds workerExecutionBoundary) *ExecutionWorkerService {
	return NewExecutionWorkerServiceWithLease(builds, "", 45*time.Second)
}

func NewExecutionWorkerServiceWithLease(builds workerExecutionBoundary, workerID string, leaseDuration time.Duration) *ExecutionWorkerService {
	resolvedWorkerID := strings.TrimSpace(workerID)
	if resolvedWorkerID == "" {
		resolvedWorkerID = uuid.NewString()
	}
	if leaseDuration <= 0 {
		leaseDuration = 45 * time.Second
	}

	return &ExecutionWorkerService{
		builds:                 builds,
		workerID:               resolvedWorkerID,
		leaseDuration:          leaseDuration,
		heartbeatWriteInterval: 5 * time.Second,
		clock:                  time.Now,
	}
}

func (w *ExecutionWorkerService) SetWorkerRepository(repo repository.WorkerRepository) {
	w.workerRepo = repo
}
