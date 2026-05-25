package worker

import (
	"context"
	"sort"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const defaultWorkerStaleAfter = 90 * time.Second

type visibilityBuildBoundary interface {
	ListBuilds(ctx context.Context) ([]domain.Build, error)
	GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error)
	GetJobsByBuildID(ctx context.Context, buildID string) ([]domain.ExecutionJob, error)
}

type VisibilityService struct {
	workers    repository.WorkerRepository
	builds     visibilityBuildBoundary
	staleAfter time.Duration
	clock      func() time.Time
}

func NewVisibilityService(workers repository.WorkerRepository, builds visibilityBuildBoundary) *VisibilityService {
	return &VisibilityService{
		workers:    workers,
		builds:     builds,
		staleAfter: defaultWorkerStaleAfter,
		clock:      time.Now,
	}
}

func (s *VisibilityService) SetStaleAfter(staleAfter time.Duration) {
	if staleAfter > 0 {
		s.staleAfter = staleAfter
	}
}

func (s *VisibilityService) ListWorkers(ctx context.Context) ([]domain.WorkerVisibility, error) {
	workers, err := s.workers.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 {
		return []domain.WorkerVisibility{}, nil
	}

	claims, err := s.collectClaims(ctx)
	if err != nil {
		return nil, err
	}

	now := s.clock().UTC()
	visible := make([]domain.WorkerVisibility, 0, len(workers))
	for _, worker := range workers {
		item := domain.WorkerVisibility{
			ID:              worker.ID,
			Name:            worker.Name,
			Status:          domain.WorkerStatusIdle,
			LastHeartbeatAt: worker.LastHeartbeatAt,
			CreatedAt:       worker.CreatedAt,
			UpdatedAt:       worker.UpdatedAt,
		}

		if now.Sub(worker.LastHeartbeatAt) > s.staleAfter {
			item.Status = domain.WorkerStatusStale
			item.StaleHeartbeat = true
		}

		if claim, ok := claims[worker.ID]; ok {
			item.CurrentBuildID = claim.BuildID
			item.CurrentBuildNum = claim.BuildNumber
			item.CurrentStepID = claim.StepID
			item.CurrentStepIndex = claim.StepIndex
			item.CurrentStepName = claim.StepName
			item.LeaseExpiresAt = claim.LeaseExpiresAt
			item.ClaimedAt = claim.ClaimedAt
			item.ProjectID = claim.ProjectID
			item.ProjectName = claim.ProjectName
			item.ProjectSlug = claim.ProjectSlug
			item.JobID = claim.JobID
			item.JobName = claim.JobName

			if claim.LeaseExpiresAt == nil || !claim.LeaseExpiresAt.After(now) {
				item.Status = domain.WorkerStatusStale
				item.StaleLease = true
			} else if item.Status != domain.WorkerStatusStale {
				item.Status = domain.WorkerStatusBusy
			}
		}

		visible = append(visible, item)
	}

	sort.Slice(visible, func(i, j int) bool {
		if visible[i].Status != visible[j].Status {
			return workerStatusRank(visible[i].Status) < workerStatusRank(visible[j].Status)
		}
		if visible[i].Name == visible[j].Name {
			return visible[i].ID < visible[j].ID
		}
		return visible[i].Name < visible[j].Name
	})

	return visible, nil
}

type workerClaim struct {
	BuildID        *string
	BuildNumber    *int64
	StepID         *string
	StepIndex      *int
	StepName       *string
	LeaseExpiresAt *time.Time
	ClaimedAt      *time.Time
	ProjectID      *string
	ProjectName    *string
	ProjectSlug    *string
	JobID          *string
	JobName        *string
	priority       int
	updatedAt      time.Time
}

func (s *VisibilityService) collectClaims(ctx context.Context) (map[string]workerClaim, error) {
	builds, err := s.builds.ListBuilds(ctx)
	if err != nil {
		return nil, err
	}

	claims := map[string]workerClaim{}
	for _, build := range builds {
		if build.Status != domain.BuildStatusPreparing && build.Status != domain.BuildStatusQueued && build.Status != domain.BuildStatusRunning {
			continue
		}

		jobs, err := s.builds.GetJobsByBuildID(ctx, build.ID)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if job.ClaimedBy == nil {
				continue
			}
			claim := workerClaim{
				BuildID:        &build.ID,
				BuildNumber:    buildNumberPtr(build.BuildNumber),
				StepID:         &job.StepID,
				StepIndex:      intPtr(job.StepIndex),
				StepName:       &job.Name,
				LeaseExpiresAt: job.ClaimExpiresAt,
				ProjectID:      &build.ProjectID,
				JobID:          build.JobID,
				priority:       2,
				updatedAt:      claimSortTime(job.StartedAt, job.CreatedAt),
			}
			mergeWorkerClaim(claims, *job.ClaimedBy, claim)
		}

		steps, err := s.builds.GetBuildSteps(ctx, build.ID)
		if err != nil {
			return nil, err
		}
		for _, step := range steps {
			if step.WorkerID == nil {
				continue
			}
			claim := workerClaim{
				BuildID:        &build.ID,
				BuildNumber:    buildNumberPtr(build.BuildNumber),
				StepID:         &step.ID,
				StepIndex:      intPtr(step.StepIndex),
				StepName:       &step.Name,
				LeaseExpiresAt: step.LeaseExpiresAt,
				ClaimedAt:      step.ClaimedAt,
				ProjectID:      &build.ProjectID,
				JobID:          build.JobID,
				priority:       1,
				updatedAt:      claimSortTime(step.ClaimedAt, build.CreatedAt),
			}
			mergeWorkerClaim(claims, *step.WorkerID, claim)
		}
	}

	return claims, nil
}

func mergeWorkerClaim(claims map[string]workerClaim, workerID string, next workerClaim) {
	current, ok := claims[workerID]
	if !ok || next.priority > current.priority || (next.priority == current.priority && next.updatedAt.After(current.updatedAt)) {
		claims[workerID] = next
	}
}

func claimSortTime(optional *time.Time, fallback time.Time) time.Time {
	if optional != nil {
		return optional.UTC()
	}
	return fallback.UTC()
}

func buildNumberPtr(value int64) *int64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func workerStatusRank(status domain.WorkerStatus) int {
	switch status {
	case domain.WorkerStatusBusy:
		return 0
	case domain.WorkerStatusIdle:
		return 1
	default:
		return 2
	}
}
