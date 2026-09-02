package memory

import (
	"context"
	"sort"
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
	builds      repository.BuildRepository
}

type buildClaimOrder struct {
	priority int
	queuedAt time.Time
	status   domain.BuildStatus
}

func NewExecutionJobRepository() *ExecutionJobRepository {
	return &ExecutionJobRepository{
		jobsByID:    map[string]domain.ExecutionJob{},
		jobsByStep:  map[string][]string{},
		jobsByBuild: map[string][]string{},
	}
}

func (r *ExecutionJobRepository) SetBuildRepository(builds repository.BuildRepository) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.builds = builds
}

func (r *ExecutionJobRepository) CreateJobsForBuild(_ context.Context, jobs []domain.ExecutionJob) ([]domain.ExecutionJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]domain.ExecutionJob, 0, len(jobs))
	for _, job := range jobs {
		job.AttemptNumber = normalizeAttemptNumber(job.AttemptNumber)
		if strings.TrimSpace(job.NodeID) == "" {
			job.NodeID = domain.FallbackNodeID(job.StepIndex)
		}
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

func (r *ExecutionJobRepository) ClaimNextRunnableJob(ctx context.Context, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	r.mu.Lock()
	builds := r.builds
	candidates, buildIDs := r.claimCandidatesLocked(claim.ClaimedAt)
	r.mu.Unlock()

	if len(candidates) == 0 {
		return domain.ExecutionJob{}, false, nil
	}

	buildOrders := loadBuildClaimOrders(ctx, builds, buildIDs)
	sort.Slice(candidates, func(i, j int) bool {
		left := buildOrders[candidates[i].BuildID]
		right := buildOrders[candidates[j].BuildID]
		if left.priority != right.priority {
			return left.priority > right.priority
		}
		if !left.queuedAt.Equal(right.queuedAt) {
			return left.queuedAt.Before(right.queuedAt)
		}
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

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, candidate := range candidates {
		buildStatus := buildOrders[candidate.BuildID].status
		if buildStatus != domain.BuildStatusQueued && buildStatus != domain.BuildStatusRunning {
			continue
		}
		job, ok := r.jobsByID[candidate.ID]
		if !ok {
			continue
		}
		latestByNode := latestJobsByNodeID(r.jobsByBuild[job.BuildID], r.jobsByID)
		if !isClaimCandidate(job, latestByNode, claim.ClaimedAt) {
			continue
		}
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

	return domain.ExecutionJob{}, false, nil
}

func (r *ExecutionJobRepository) claimCandidatesLocked(now time.Time) ([]domain.ExecutionJob, map[string]struct{}) {
	candidates := make([]domain.ExecutionJob, 0)
	buildIDs := make(map[string]struct{})
	runnableByBuild := make(map[string]map[string]domain.ExecutionJob)
	for _, job := range r.jobsByID {
		latestByNode, ok := runnableByBuild[job.BuildID]
		if !ok {
			latestByNode = latestJobsByNodeID(r.jobsByBuild[job.BuildID], r.jobsByID)
			runnableByBuild[job.BuildID] = latestByNode
		}
		if !isClaimCandidate(job, latestByNode, now) {
			continue
		}
		candidates = append(candidates, job)
		buildIDs[job.BuildID] = struct{}{}
	}
	return candidates, buildIDs
}

func loadBuildClaimOrders(ctx context.Context, builds repository.BuildRepository, buildIDs map[string]struct{}) map[string]buildClaimOrder {
	orders := make(map[string]buildClaimOrder, len(buildIDs))
	for buildID := range buildIDs {
		orders[buildID] = defaultBuildClaimOrder()
	}
	if builds == nil {
		return orders
	}
	for buildID := range buildIDs {
		build, err := builds.GetByID(ctx, buildID)
		if err != nil {
			continue
		}
		queuedAt := build.CreatedAt
		if build.QueuedAt != nil {
			queuedAt = *build.QueuedAt
		}
		orders[buildID] = buildClaimOrder{
			priority: domain.NormalizePriority(build.Priority),
			queuedAt: queuedAt,
			status:   build.Status,
		}
	}
	return orders
}

func defaultBuildClaimOrder() buildClaimOrder {
	return buildClaimOrder{priority: domain.DefaultPriority, status: domain.BuildStatusRunning}
}

func isClaimCandidate(job domain.ExecutionJob, latestByNode map[string]domain.ExecutionJob, now time.Time) bool {
	if !isJobRunnable(job, latestByNode) {
		return false
	}
	if job.Status == domain.ExecutionJobStatusQueued {
		return true
	}
	return job.Status == domain.ExecutionJobStatusRunning && job.ClaimExpiresAt != nil && !job.ClaimExpiresAt.After(now)
}

func (r *ExecutionJobRepository) ClaimJobByStepID(ctx context.Context, stepID string, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
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
	if r.builds != nil {
		build, err := r.builds.GetByID(ctx, job.BuildID)
		if err != nil {
			return cloneExecutionJob(job), false, nil
		}
		if build.Status != domain.BuildStatusRunning {
			return cloneExecutionJob(job), false, nil
		}
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
	if domain.IsTerminalExecutionJobStatus(job.Status) {
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

	job, outcome, err := r.completeJobLocked(jobID, claimToken, finishedAt, domain.ExecutionJobStatusSuccess, nil, nil, &exitCode, outputRefs)
	return job, outcome, err
}

func (r *ExecutionJobRepository) CompleteSuccessfulStepAndJob(_ context.Context, request repository.CompleteSuccessfulStepAndJobRequest) (repository.CompleteStepResult, domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	job, ok := r.jobsByID[request.JobID]
	if !ok {
		return repository.CompleteStepResult{}, domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrExecutionJobNotFound
	}
	if domain.IsTerminalExecutionJobStatus(job.Status) {
		return repository.CompleteStepResult{}, cloneExecutionJob(job), repository.StepCompletionDuplicateTerminal, nil
	}
	if job.Status != domain.ExecutionJobStatusRunning || job.ClaimToken == nil || *job.ClaimToken != request.ClaimToken || job.ClaimExpiresAt == nil || !job.ClaimExpiresAt.After(time.Now().UTC()) {
		return repository.CompleteStepResult{}, cloneExecutionJob(job), repository.StepCompletionStaleClaim, nil
	}
	builds, ok := r.builds.(*BuildRepository)
	if !ok || builds == nil {
		return repository.CompleteStepResult{}, domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
	}

	builds.mu.Lock()
	defer builds.mu.Unlock()

	stepResult, stepErr := builds.completeStepLocked(request.StepRequest)
	if stepErr != nil || stepResult.Outcome != repository.StepCompletionCompleted {
		return stepResult, domain.ExecutionJob{}, stepResult.Outcome, stepErr
	}
	jobResult, outcome, jobErr := r.completeJobLocked(request.JobID, request.ClaimToken, request.FinishedAt, domain.ExecutionJobStatusSuccess, nil, nil, &request.ExitCode, nil)
	return stepResult, jobResult, outcome, jobErr
}

func (r *ExecutionJobRepository) CompleteFailedStepAndJob(_ context.Context, request repository.CompleteFailedStepAndJobRequest) (repository.CompleteStepResult, domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	job, ok := r.jobsByID[request.JobID]
	if !ok {
		return repository.CompleteStepResult{}, domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrExecutionJobNotFound
	}
	if domain.IsTerminalExecutionJobStatus(job.Status) {
		return repository.CompleteStepResult{}, cloneExecutionJob(job), repository.StepCompletionDuplicateTerminal, nil
	}
	if job.Status != domain.ExecutionJobStatusRunning || job.ClaimToken == nil || *job.ClaimToken != request.ClaimToken || job.ClaimExpiresAt == nil || !job.ClaimExpiresAt.After(time.Now().UTC()) {
		return repository.CompleteStepResult{}, cloneExecutionJob(job), repository.StepCompletionStaleClaim, nil
	}
	builds, ok := r.builds.(*BuildRepository)
	if !ok || builds == nil {
		return repository.CompleteStepResult{}, domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
	}

	builds.mu.Lock()
	defer builds.mu.Unlock()

	stepResult, stepErr := builds.completeStepLocked(request.StepRequest)
	if stepErr != nil || stepResult.Outcome != repository.StepCompletionCompleted {
		return stepResult, domain.ExecutionJob{}, stepResult.Outcome, stepErr
	}
	message := request.ErrorMessage
	jobResult, outcome, jobErr := r.completeJobLocked(request.JobID, request.ClaimToken, request.FinishedAt, domain.ExecutionJobStatusFailed, &message, &request.FailureKind, request.ExitCode, nil)
	return stepResult, jobResult, outcome, jobErr
}

func (r *ExecutionJobRepository) CompleteJobFailure(_ context.Context, jobID string, claimToken string, finishedAt time.Time, errorMessage string, failureKind domain.ExecutionFailureKind, exitCode *int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := errorMessage
	job, outcome, err := r.completeJobLocked(jobID, claimToken, finishedAt, domain.ExecutionJobStatusFailed, &msg, &failureKind, exitCode, outputRefs)
	return job, outcome, err
}

func (r *ExecutionJobRepository) completeJobLocked(jobID string, claimToken string, finishedAt time.Time, status domain.ExecutionJobStatus, errorMessage *string, failureKind *domain.ExecutionFailureKind, exitCode *int, outputRefs []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	job, ok := r.jobsByID[jobID]
	if !ok {
		return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repository.ErrExecutionJobNotFound
	}
	if domain.IsTerminalExecutionJobStatus(job.Status) {
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
	job.FailureKind = failureKind
	job.ExitCode = exitCode
	job.OutputRefs = cloneArtifactRefs(outputRefs)
	job.ClaimToken = nil
	job.ClaimedBy = nil
	job.ClaimExpiresAt = nil
	r.jobsByID[jobID] = job
	return cloneExecutionJob(job), repository.StepCompletionCompleted, nil
}

func (r *ExecutionJobRepository) CancelJobsForBuild(_ context.Context, buildID string, reason string, canceledAt time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	updated := 0
	for _, id := range r.jobsByBuild[buildID] {
		job, ok := r.jobsByID[id]
		if !ok || !domain.CanCancelExecutionJob(job.Status) {
			continue
		}
		job.Status = domain.ExecutionJobStatusCanceled
		job.FailureKind = nil
		job.ClaimToken = nil
		job.ClaimedBy = nil
		job.ClaimExpiresAt = nil
		if job.StartedAt == nil {
			started := canceledAt
			job.StartedAt = &started
		}
		if job.FinishedAt == nil {
			finished := canceledAt
			job.FinishedAt = &finished
		}
		if strings.TrimSpace(reason) != "" {
			message := strings.TrimSpace(reason)
			job.ErrorMessage = &message
		}
		r.jobsByID[id] = job
		updated++
	}

	return updated, nil
}

func cloneExecutionJob(job domain.ExecutionJob) domain.ExecutionJob {
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
	if job.FailureKind != nil {
		value := *job.FailureKind
		job.FailureKind = &value
	}
	job.OutputRefs = cloneArtifactRefs(job.OutputRefs)
	return job
}

func latestJobsByNodeID(jobIDs []string, jobsByID map[string]domain.ExecutionJob) map[string]domain.ExecutionJob {
	latest := make(map[string]domain.ExecutionJob, len(jobIDs))
	for _, id := range jobIDs {
		job, ok := jobsByID[id]
		if !ok {
			continue
		}
		nodeID := strings.TrimSpace(job.NodeID)
		if nodeID == "" {
			nodeID = domain.FallbackNodeID(job.StepIndex)
		}
		existing, found := latest[nodeID]
		if !found || job.AttemptNumber > existing.AttemptNumber || (job.AttemptNumber == existing.AttemptNumber && job.CreatedAt.After(existing.CreatedAt)) {
			latest[nodeID] = job
		}
	}
	return latest
}

func isJobRunnable(job domain.ExecutionJob, latestByNode map[string]domain.ExecutionJob) bool {
	for _, dep := range job.DependsOnNodeIDs {
		upstream, ok := latestByNode[strings.TrimSpace(dep)]
		if !ok || upstream.Status != domain.ExecutionJobStatusSuccess {
			return false
		}
	}
	return true
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
