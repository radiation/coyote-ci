package worker

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const defaultWorkerStaleAfter = 90 * time.Second

type visibilityBuildBoundary interface {
	ListActiveBuilds(ctx context.Context) ([]domain.Build, error)
	GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error)
	GetJobsByBuildID(ctx context.Context, buildID string) ([]domain.ExecutionJob, error)
}

type VisibilityService struct {
	workers    repository.WorkerRepository
	builds     visibilityBuildBoundary
	projects   repository.ProjectRepository
	jobs       repository.JobRepository
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

func (s *VisibilityService) SetProjectRepository(projects repository.ProjectRepository) {
	s.projects = projects
}

func (s *VisibilityService) SetJobRepository(jobs repository.JobRepository) {
	s.jobs = jobs
}

func (s *VisibilityService) ListWorkers(ctx context.Context) ([]domain.WorkerVisibility, error) {
	workers, err := s.workers.List(ctx)
	if err != nil {
		return nil, err
	}

	claims, err := s.collectClaims(ctx)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 && len(claims) == 0 {
		return []domain.WorkerVisibility{}, nil
	}

	now := s.clock().UTC()
	visible := make([]domain.WorkerVisibility, 0, len(workers)+len(claims))
	seen := make(map[string]struct{}, len(workers))
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
		seen[worker.ID] = struct{}{}
	}

	for workerID, claim := range claims {
		if _, ok := seen[workerID]; ok {
			continue
		}

		visible = append(visible, domain.WorkerVisibility{
			ID:               workerID,
			Name:             workerID,
			Status:           orphanClaimStatus(claim, now),
			CreatedAt:        claim.updatedAt,
			UpdatedAt:        claim.updatedAt,
			CurrentBuildID:   claim.BuildID,
			CurrentBuildNum:  claim.BuildNumber,
			CurrentStepID:    claim.StepID,
			CurrentStepIndex: claim.StepIndex,
			CurrentStepName:  claim.StepName,
			LeaseExpiresAt:   claim.LeaseExpiresAt,
			ClaimedAt:        claim.ClaimedAt,
			ProjectID:        claim.ProjectID,
			ProjectName:      claim.ProjectName,
			ProjectSlug:      claim.ProjectSlug,
			JobID:            claim.JobID,
			JobName:          claim.JobName,
			StaleLease:       claim.LeaseExpiresAt == nil || !claim.LeaseExpiresAt.After(now),
			StaleHeartbeat:   true,
		})
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
	// TODO: If the active build set grows, add a repository-level read optimized
	// for active worker visibility instead of walking all builds and loading
	// jobs/steps per active build.
	builds, err := s.builds.ListActiveBuilds(ctx)
	if err != nil {
		return nil, err
	}

	claims := map[string]workerClaim{}
	projectCache := map[string]projectMeta{}
	jobCache := map[string]jobMeta{}
	for _, build := range builds {
		jobs, err := s.builds.GetJobsByBuildID(ctx, build.ID)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if !isActiveJobClaim(job) {
				continue
			}
			var projectInfo projectMeta
			projectInfo, err = s.loadProjectMeta(ctx, build.ProjectID, projectCache)
			if err != nil {
				return nil, err
			}
			var jobInfo jobMeta
			jobInfo, err = s.loadJobMeta(ctx, build.JobID, jobCache)
			if err != nil {
				return nil, err
			}
			claim := workerClaim{
				BuildID:        &build.ID,
				BuildNumber:    buildNumberPtr(build.BuildNumber),
				StepID:         &job.StepID,
				StepIndex:      intPtr(job.StepIndex),
				StepName:       &job.Name,
				LeaseExpiresAt: job.ClaimExpiresAt,
				ProjectID:      &build.ProjectID,
				ProjectName:    projectInfo.name,
				ProjectSlug:    projectInfo.slug,
				JobID:          build.JobID,
				JobName:        jobInfo.name,
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
			if !isActiveStepClaim(step) {
				continue
			}
			var projectInfo projectMeta
			projectInfo, err = s.loadProjectMeta(ctx, build.ProjectID, projectCache)
			if err != nil {
				return nil, err
			}
			var jobInfo jobMeta
			jobInfo, err = s.loadJobMeta(ctx, build.JobID, jobCache)
			if err != nil {
				return nil, err
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
				ProjectName:    projectInfo.name,
				ProjectSlug:    projectInfo.slug,
				JobID:          build.JobID,
				JobName:        jobInfo.name,
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

func isActiveJobClaim(job domain.ExecutionJob) bool {
	return job.Status == domain.ExecutionJobStatusRunning && job.ClaimedBy != nil
}

func isActiveStepClaim(step domain.BuildStep) bool {
	return step.Status == domain.BuildStepStatusRunning && step.WorkerID != nil
}

func orphanClaimStatus(claim workerClaim, now time.Time) domain.WorkerStatus {
	// A claim without a heartbeat registry row is surfaced as stale so partial
	// rollout/backfill still exposes owned work without implying health.
	return domain.WorkerStatusStale
}

type projectMeta struct {
	name *string
	slug *string
}

type jobMeta struct {
	name *string
}

func (s *VisibilityService) loadProjectMeta(ctx context.Context, projectID string, cache map[string]projectMeta) (projectMeta, error) {
	if projectID == "" || s.projects == nil {
		return projectMeta{}, nil
	}
	if meta, ok := cache[projectID]; ok {
		return meta, nil
	}

	project, err := s.projects.GetByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, repository.ErrProjectNotFound) {
			cache[projectID] = projectMeta{}
			return projectMeta{}, nil
		}
		return projectMeta{}, err
	}

	meta := projectMeta{name: stringPointer(project.Name), slug: stringPointer(project.Slug)}
	cache[projectID] = meta
	return meta, nil
}

func (s *VisibilityService) loadJobMeta(ctx context.Context, jobID *string, cache map[string]jobMeta) (jobMeta, error) {
	if jobID == nil || *jobID == "" || s.jobs == nil {
		return jobMeta{}, nil
	}
	if meta, ok := cache[*jobID]; ok {
		return meta, nil
	}

	job, err := s.jobs.GetByID(ctx, *jobID)
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			cache[*jobID] = jobMeta{}
			return jobMeta{}, nil
		}
		return jobMeta{}, err
	}

	meta := jobMeta{name: stringPointer(job.Name)}
	cache[*jobID] = meta
	return meta, nil
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
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
