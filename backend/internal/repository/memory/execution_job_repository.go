package memory

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ExecutionJobRepository struct {
	mu          sync.RWMutex
	jobsByID    map[string]domain.ExecutionJob
	jobsByStep  map[string][]string
	jobsByBuild map[string][]string
}

func NewExecutionJobRepository() *ExecutionJobRepository {
	return &ExecutionJobRepository{
		jobsByID:    map[string]domain.ExecutionJob{},
		jobsByStep:  map[string][]string{},
		jobsByBuild: map[string][]string{},
	}
}

func (r *ExecutionJobRepository) CreateJobsForBuild(_ context.Context, jobs []domain.ExecutionJob) ([]domain.ExecutionJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]domain.ExecutionJob, 0, len(jobs))
	for _, job := range jobs {
		job.AttemptNumber = normalizeAttemptNumber(job.AttemptNumber)
		r.jobsByID[job.ID] = cloneExecutionJob(job)
		r.jobsByStep[job.StepID] = append(r.jobsByStep[job.StepID], job.ID)
		r.jobsByBuild[job.BuildID] = append(r.jobsByBuild[job.BuildID], job.ID)
		out = append(out, cloneExecutionJob(job))
	}

	r.sortBuildAndStepJobsLocked(jobs)
	return out, nil
}

func (r *ExecutionJobRepository) sortBuildAndStepJobsLocked(jobs []domain.ExecutionJob) {
	buildSeen := map[string]struct{}{}
	stepSeen := map[string]struct{}{}

	for _, job := range jobs {
		if _, ok := buildSeen[job.BuildID]; !ok {
			buildSeen[job.BuildID] = struct{}{}
			ids := r.jobsByBuild[job.BuildID]
			sort.Slice(ids, func(i, j int) bool {
				left := r.jobsByID[ids[i]]
				right := r.jobsByID[ids[j]]
				if left.StepIndex == right.StepIndex {
					if left.AttemptNumber == right.AttemptNumber {
						if left.CreatedAt.Equal(right.CreatedAt) {
							return left.ID < right.ID
						}
						return left.CreatedAt.Before(right.CreatedAt)
					}
					return left.AttemptNumber < right.AttemptNumber
				}
				return left.StepIndex < right.StepIndex
			})
			r.jobsByBuild[job.BuildID] = ids
		}

		if _, ok := stepSeen[job.StepID]; !ok {
			stepSeen[job.StepID] = struct{}{}
			ids := r.jobsByStep[job.StepID]
			sort.Slice(ids, func(i, j int) bool {
				left := r.jobsByID[ids[i]]
				right := r.jobsByID[ids[j]]
				if left.AttemptNumber == right.AttemptNumber {
					if left.CreatedAt.Equal(right.CreatedAt) {
						return left.ID < right.ID
					}
					return left.CreatedAt.Before(right.CreatedAt)
				}
				return left.AttemptNumber < right.AttemptNumber
			})
			r.jobsByStep[job.StepID] = ids
		}
	}
}

func (r *ExecutionJobRepository) GetJobsByBuildID(_ context.Context, buildID string) ([]domain.ExecutionJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.jobsByBuild[buildID]
	out := make([]domain.ExecutionJob, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneExecutionJob(r.jobsByID[id]))
	}
	return out, nil
}

func (r *ExecutionJobRepository) GetJobByID(_ context.Context, id string) (domain.ExecutionJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	job, ok := r.jobsByID[id]
	if !ok {
		return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
	}
	return cloneExecutionJob(job), nil
}

func (r *ExecutionJobRepository) GetJobByStepID(_ context.Context, stepID string) (domain.ExecutionJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.jobsByStep[stepID]
	if len(ids) == 0 {
		return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
	}
	job := r.jobsByID[ids[len(ids)-1]]
	return cloneExecutionJob(job), nil
}

func (r *ExecutionJobRepository) ClaimNextRunnableJob(_ context.Context, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := claim.ClaimedAt
	candidates := make([]domain.ExecutionJob, 0)
	runnableByBuild := make(map[string]map[string]domain.ExecutionJob)
	for _, job := range r.jobsByID {
		latestByNode, ok := runnableByBuild[job.BuildID]
		if !ok {
			latestByNode = latestJobsByNodeID(r.jobsByBuild[job.BuildID], r.jobsByID)
			runnableByBuild[job.BuildID] = latestByNode
		}
		if !isJobRunnable(job, latestByNode) {
			continue
		}
		if job.Status == domain.ExecutionJobStatusQueued {
			candidates = append(candidates, job)
			continue
		}
		if job.Status == domain.ExecutionJobStatusRunning && job.ClaimExpiresAt != nil && !job.ClaimExpiresAt.After(now) {
			candidates = append(candidates, job)
		}
	}

	if len(candidates) == 0 {
		return domain.ExecutionJob{}, false, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			if candidates[i].StepIndex == candidates[j].StepIndex {
				if candidates[i].AttemptNumber == candidates[j].AttemptNumber {
					return candidates[i].ID < candidates[j].ID
				}
				return candidates[i].AttemptNumber < candidates[j].AttemptNumber
			}
			return candidates[i].StepIndex < candidates[j].StepIndex
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	job := candidates[0]
	job.Status = domain.ExecutionJobStatusRunning
	job.ClaimedBy = &claim.WorkerID
	job.ClaimToken = &claim.ClaimToken
	job.ClaimExpiresAt = &claim.LeaseExpiresAt
	if job.StartedAt == nil {
		started := claim.ClaimedAt
		job.StartedAt = &started
	}
	r.jobsByID[job.ID] = job
	return cloneExecutionJob(job), true, nil
}

func (r *ExecutionJobRepository) ClaimJobByStepID(_ context.Context, stepID string, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := r.jobsByStep[stepID]
	if len(ids) == 0 {
		return domain.ExecutionJob{}, false, repository.ErrExecutionJobNotFound
	}
	id := ids[len(ids)-1]
	job, ok := r.jobsByID[id]
	if !ok {
		return domain.ExecutionJob{}, false, repository.ErrExecutionJobNotFound
	}
	if job.Status != domain.ExecutionJobStatusQueued && job.Status != domain.ExecutionJobStatusRunning {
		return cloneExecutionJob(job), false, nil
	}

	job.Status = domain.ExecutionJobStatusRunning
	job.ClaimedBy = &claim.WorkerID
	job.ClaimToken = &claim.ClaimToken
	job.ClaimExpiresAt = &claim.LeaseExpiresAt
	if job.StartedAt == nil {
		started := claim.ClaimedAt
		job.StartedAt = &started
	}
	r.jobsByID[id] = job
	return cloneExecutionJob(job), true, nil
}

func (r *ExecutionJobRepository) RenewJobLease(_ context.Context, jobID string, claimToken string, leaseExpiresAt time.Time) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	job, ok := r.jobsByID[jobID]
	if !ok {
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrExecutionJobNotFound
	}
	if job.Status == domain.ExecutionJobStatusSuccess || job.Status == domain.ExecutionJobStatusFailed {
		return cloneExecutionJob(job), repository.StepCompletionDuplicateTerminal, nil
	}
	if job.Status != domain.ExecutionJobStatusRunning {
		return cloneExecutionJob(job), repository.StepCompletionInvalidTransition, nil
	}
	if job.ClaimToken == nil || *job.ClaimToken != claimToken {
		return cloneExecutionJob(job), repository.StepCompletionStaleClaim, nil
	}

	job.ClaimExpiresAt = &leaseExpiresAt
	r.jobsByID[jobID] = job
	return cloneExecutionJob(job), repository.StepCompletionCompleted, nil
}

func (r *ExecutionJobRepository) CompleteJobSuccess(_ context.Context, jobID string, claimToken string, finishedAt time.Time, exitCode int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	job, outcome, err := r.completeJobLocked(jobID, claimToken, finishedAt, domain.ExecutionJobStatusSuccess, nil, &exitCode, outputRefs)
	return job, outcome, err
}

func (r *ExecutionJobRepository) CompleteJobFailure(_ context.Context, jobID string, claimToken string, finishedAt time.Time, errorMessage string, exitCode *int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := errorMessage
	job, outcome, err := r.completeJobLocked(jobID, claimToken, finishedAt, domain.ExecutionJobStatusFailed, &msg, exitCode, outputRefs)
	return job, outcome, err
}

func (r *ExecutionJobRepository) completeJobLocked(jobID string, claimToken string, finishedAt time.Time, status domain.ExecutionJobStatus, errorMessage *string, exitCode *int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	job, ok := r.jobsByID[jobID]
	if !ok {
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrExecutionJobNotFound
	}
	if job.Status == domain.ExecutionJobStatusSuccess || job.Status == domain.ExecutionJobStatusFailed {
		return cloneExecutionJob(job), repository.StepCompletionDuplicateTerminal, nil
	}
	if job.Status != domain.ExecutionJobStatusRunning {
		return cloneExecutionJob(job), repository.StepCompletionInvalidTransition, nil
	}
	if job.ClaimToken == nil || *job.ClaimToken != claimToken {
		return cloneExecutionJob(job), repository.StepCompletionStaleClaim, nil
	}

	job.Status = status
	job.FinishedAt = &finishedAt
	job.ErrorMessage = errorMessage
	job.ExitCode = exitCode
	job.OutputRefs = cloneArtifactRefs(outputRefs)
	job.ClaimToken = nil
	job.ClaimedBy = nil
	job.ClaimExpiresAt = nil
	r.jobsByID[jobID] = job
	return cloneExecutionJob(job), repository.StepCompletionCompleted, nil
}

func cloneExecutionJob(job domain.ExecutionJob) domain.ExecutionJob {
	if job.GroupName != nil {
		group := *job.GroupName
		job.GroupName = &group
	}
	if job.DependsOnNodeIDs != nil {
		job.DependsOnNodeIDs = append([]string(nil), job.DependsOnNodeIDs...)
	}
	if job.Command != nil {
		job.Command = append([]string(nil), job.Command...)
	}
	if job.Environment != nil {
		env := make(map[string]string, len(job.Environment))
		for k, v := range job.Environment {
			env[k] = v
		}
		job.Environment = env
	}
	job.OutputRefs = cloneArtifactRefs(job.OutputRefs)
	return job
}

func cloneArtifactRefs(in []domain.ArtifactRef) []domain.ArtifactRef {
	if len(in) == 0 {
		return []domain.ArtifactRef{}
	}
	out := make([]domain.ArtifactRef, len(in))
	copy(out, in)
	return out
}

func normalizeAttemptNumber(value int) int {
	if value < 1 {
		return 1
	}
	return value
}

func latestJobsByNodeID(ids []string, jobsByID map[string]domain.ExecutionJob) map[string]domain.ExecutionJob {
	out := make(map[string]domain.ExecutionJob, len(ids))
	for _, id := range ids {
		job, ok := jobsByID[id]
		if !ok {
			continue
		}
		nodeID := normalizedJobNodeID(job)
		existing, exists := out[nodeID]
		if !exists {
			out[nodeID] = job
			continue
		}
		if existing.AttemptNumber < job.AttemptNumber {
			out[nodeID] = job
			continue
		}
		if existing.AttemptNumber == job.AttemptNumber {
			if existing.CreatedAt.Before(job.CreatedAt) {
				out[nodeID] = job
				continue
			}
			if existing.CreatedAt.Equal(job.CreatedAt) && existing.ID < job.ID {
				out[nodeID] = job
			}
		}
	}
	return out
}

func normalizedJobNodeID(job domain.ExecutionJob) string {
	nodeID := strings.TrimSpace(job.NodeID)
	if nodeID == "" {
		return "step-" + strconv.Itoa(job.StepIndex)
	}
	return nodeID
}

func isJobRunnable(job domain.ExecutionJob, latestByNode map[string]domain.ExecutionJob) bool {
	if len(job.DependsOnNodeIDs) > 0 {
		for _, dep := range job.DependsOnNodeIDs {
			dependency, ok := latestByNode[strings.TrimSpace(dep)]
			if !ok || dependency.Status != domain.ExecutionJobStatusSuccess {
				return false
			}
		}
		return true
	}

	for _, previous := range latestByNode {
		if previous.StepIndex >= job.StepIndex {
			continue
		}
		if previous.Status != domain.ExecutionJobStatusSuccess {
			return false
		}
	}
	return true
}
