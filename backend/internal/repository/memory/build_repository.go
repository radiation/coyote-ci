package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type BuildRepository struct {
	mu         sync.RWMutex
	builds     map[string]domain.Build
	buildSteps map[string][]domain.BuildStep
}

func NewBuildRepository() *BuildRepository {
	return &BuildRepository{
		builds:     make(map[string]domain.Build),
		buildSteps: make(map[string][]domain.BuildStep),
	}
}

func (r *BuildRepository) Create(_ context.Context, build domain.Build) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if build.ID == "" {
		build.ID = uuid.NewString()
	}
	if build.AttemptNumber <= 0 {
		build.AttemptNumber = 1
	}
	build.Trigger = domain.NormalizeBuildTrigger(build.Trigger)

	r.builds[build.ID] = build
	return build, nil
}

func (r *BuildRepository) CreateQueuedBuild(_ context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if build.ID == "" {
		build.ID = uuid.NewString()
	}
	if build.AttemptNumber <= 0 {
		build.AttemptNumber = 1
	}
	build.Trigger = domain.NormalizeBuildTrigger(build.Trigger)

	now := time.Now().UTC()
	build.Status = domain.BuildStatusQueued
	build.CurrentStepIndex = 0
	build.ErrorMessage = nil
	if build.QueuedAt == nil {
		build.QueuedAt = &now
	}

	r.builds[build.ID] = build

	cloned := make([]domain.BuildStep, 0, len(steps))
	for _, step := range steps {
		if step.ID == "" {
			step.ID = uuid.NewString()
		}
		step.BuildID = build.ID
		cloned = append(cloned, cloneStep(step))
	}

	sort.Slice(cloned, func(i, j int) bool {
		return cloned[i].StepIndex < cloned[j].StepIndex
	})

	r.buildSteps[build.ID] = cloned

	return build, nil
}

func (r *BuildRepository) List(_ context.Context) ([]domain.Build, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	builds := make([]domain.Build, 0, len(r.builds))
	for _, build := range r.builds {
		builds = append(builds, build)
	}

	sort.Slice(builds, func(i, j int) bool {
		if builds[i].CreatedAt.Equal(builds[j].CreatedAt) {
			return builds[i].ID < builds[j].ID
		}
		return builds[i].CreatedAt.After(builds[j].CreatedAt)
	})

	return builds, nil
}

func (r *BuildRepository) ListPaged(_ context.Context, params repository.ListParams) ([]domain.Build, error) {
	all, err := r.List(context.Background())
	if err != nil {
		return nil, err
	}

	limit, offset := clampMemoryPageParams(params)
	if offset >= len(all) {
		return []domain.Build{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (r *BuildRepository) ListByJobID(_ context.Context, jobID string) ([]domain.Build, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	builds := make([]domain.Build, 0)
	for _, build := range r.builds {
		if build.JobID != nil && *build.JobID == jobID {
			builds = append(builds, build)
		}
	}

	sort.Slice(builds, func(i, j int) bool {
		if builds[i].CreatedAt.Equal(builds[j].CreatedAt) {
			return builds[i].ID < builds[j].ID
		}
		return builds[i].CreatedAt.After(builds[j].CreatedAt)
	})

	return builds, nil
}

func (r *BuildRepository) GetByID(_ context.Context, id string) (domain.Build, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	return build, nil
}

func (r *BuildRepository) UpdateStatus(_ context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	now := time.Now().UTC()
	build.Status = status
	if status == domain.BuildStatusQueued && build.QueuedAt == nil {
		build.QueuedAt = &now
	}
	if status == domain.BuildStatusRunning && build.StartedAt == nil {
		build.StartedAt = &now
	}
	if status == domain.BuildStatusSuccess || status == domain.BuildStatusFailed {
		build.FinishedAt = &now
	}
	if status == domain.BuildStatusFailed {
		build.ErrorMessage = errorMessage
	} else {
		build.ErrorMessage = nil
	}

	r.builds[id] = build

	return build, nil
}

func (r *BuildRepository) UpdateSourceCommitSHA(_ context.Context, id string, commitSHA string) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	trimmed := strings.TrimSpace(commitSHA)
	if trimmed == "" {
		build.CommitSHA = nil
	} else {
		build.CommitSHA = &trimmed
	}
	build.Source = domain.NewSourceSpec(readOptionalString(build.RepoURL), readOptionalString(build.Ref), readOptionalString(build.CommitSHA))

	r.builds[id] = build
	return build, nil
}

func (r *BuildRepository) QueueBuild(_ context.Context, id string, steps []domain.BuildStep) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	now := time.Now().UTC()
	build.Status = domain.BuildStatusQueued
	if build.QueuedAt == nil {
		build.QueuedAt = &now
	}
	build.CurrentStepIndex = 0
	build.ErrorMessage = nil
	r.builds[id] = build

	cloned := make([]domain.BuildStep, 0, len(steps))
	for _, step := range steps {
		if step.ID == "" {
			step.ID = uuid.NewString()
		}
		step.BuildID = id
		cloned = append(cloned, cloneStep(step))
	}

	sort.Slice(cloned, func(i, j int) bool {
		return cloned[i].StepIndex < cloned[j].StepIndex
	})

	r.buildSteps[id] = cloned

	return build, nil
}

func (r *BuildRepository) GetStepsByBuildID(_ context.Context, buildID string) ([]domain.BuildStep, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, ok := r.builds[buildID]; !ok {
		return nil, repository.ErrBuildNotFound
	}

	steps := r.buildSteps[buildID]
	out := make([]domain.BuildStep, len(steps))
	for i := range steps {
		out[i] = cloneStep(steps[i])
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].StepIndex < out[j].StepIndex
	})

	return out, nil
}

func (r *BuildRepository) ClaimStepIfPending(_ context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.builds[buildID]; !ok {
		return domain.BuildStep{}, false, repository.ErrBuildNotFound
	}
	if !domain.CanTransitionStep(domain.BuildStepStatusPending, domain.BuildStepStatusRunning) {
		return domain.BuildStep{}, false, repository.ErrInvalidBuildStepTransition
	}

	steps := r.buildSteps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}

		if steps[idx].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}

		steps[idx].Status = domain.BuildStepStatusRunning
		if workerID != nil {
			steps[idx].WorkerID = workerID
		}
		steps[idx].StartedAt = &startedAt
		r.buildSteps[buildID] = steps

		return cloneStep(steps[idx]), true, nil
	}

	return domain.BuildStep{}, false, nil
}

func (r *BuildRepository) ClaimPendingStep(_ context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.builds[buildID]; !ok {
		return domain.BuildStep{}, false, repository.ErrBuildNotFound
	}
	if !domain.CanTransitionStep(domain.BuildStepStatusPending, domain.BuildStepStatusRunning) {
		return domain.BuildStep{}, false, repository.ErrInvalidBuildStepTransition
	}

	steps := r.buildSteps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}

		if steps[idx].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}

		steps[idx].Status = domain.BuildStepStatusRunning
		steps[idx].WorkerID = &claim.WorkerID
		steps[idx].ClaimToken = &claim.ClaimToken
		steps[idx].ClaimedAt = &claim.ClaimedAt
		steps[idx].LeaseExpiresAt = &claim.LeaseExpiresAt
		if steps[idx].StartedAt == nil {
			steps[idx].StartedAt = &claim.ClaimedAt
		}
		r.buildSteps[buildID] = steps

		return cloneStep(steps[idx]), true, nil
	}

	return domain.BuildStep{}, false, nil
}

func (r *BuildRepository) ReclaimExpiredStep(_ context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.builds[buildID]; !ok {
		return domain.BuildStep{}, false, repository.ErrBuildNotFound
	}

	steps := r.buildSteps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}

		if steps[idx].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false, nil
		}
		if steps[idx].LeaseExpiresAt == nil || steps[idx].LeaseExpiresAt.After(reclaimBefore) {
			return domain.BuildStep{}, false, nil
		}

		steps[idx].WorkerID = &claim.WorkerID
		steps[idx].ClaimToken = &claim.ClaimToken
		steps[idx].ClaimedAt = &claim.ClaimedAt
		steps[idx].LeaseExpiresAt = &claim.LeaseExpiresAt
		r.buildSteps[buildID] = steps

		return cloneStep(steps[idx]), true, nil
	}

	return domain.BuildStep{}, false, nil
}

func (r *BuildRepository) RenewStepLease(_ context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.builds[buildID]; !ok {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
	}

	steps := r.buildSteps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}

		if domain.IsTerminalStepStatus(steps[idx].Status) {
			return cloneStep(steps[idx]), repository.StepCompletionDuplicateTerminal, nil
		}
		if steps[idx].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
		if steps[idx].ClaimToken == nil || *steps[idx].ClaimToken != claimToken {
			return cloneStep(steps[idx]), repository.StepCompletionStaleClaim, nil
		}

		steps[idx].LeaseExpiresAt = &leaseExpiresAt
		r.buildSteps[buildID] = steps
		return cloneStep(steps[idx]), repository.StepCompletionCompleted, nil
	}

	return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
}

func (r *BuildRepository) UpdateStepByIndex(_ context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.builds[buildID]; !ok {
		return domain.BuildStep{}, repository.ErrBuildNotFound
	}

	steps := r.buildSteps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}
		steps[idx].Status = update.Status
		if update.WorkerID != nil {
			steps[idx].WorkerID = update.WorkerID
		}
		if update.ExitCode != nil {
			steps[idx].ExitCode = update.ExitCode
		}
		if update.Stdout != nil {
			steps[idx].Stdout = update.Stdout
		}
		if update.Stderr != nil {
			steps[idx].Stderr = update.Stderr
		}
		if update.Status == domain.BuildStepStatusFailed {
			steps[idx].ErrorMessage = update.ErrorMessage
		} else {
			steps[idx].ErrorMessage = nil
		}
		if update.StartedAt != nil {
			steps[idx].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			steps[idx].FinishedAt = update.FinishedAt
		}

		r.buildSteps[buildID] = steps
		return cloneStep(steps[idx]), nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
}

func (r *BuildRepository) CompleteStepIfRunning(_ context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.builds[buildID]; !ok {
		return domain.BuildStep{}, false, repository.ErrBuildNotFound
	}

	steps := r.buildSteps[buildID]
	for idx := range steps {
		if steps[idx].StepIndex != stepIndex {
			continue
		}

		if steps[idx].Status != domain.BuildStepStatusRunning {
			return cloneStep(steps[idx]), false, nil
		}
		if !domain.CanTransitionStep(steps[idx].Status, update.Status) {
			return domain.BuildStep{}, false, repository.ErrInvalidBuildStepTransition
		}

		steps[idx].Status = update.Status
		if update.WorkerID != nil {
			steps[idx].WorkerID = update.WorkerID
		}
		if update.ExitCode != nil {
			steps[idx].ExitCode = update.ExitCode
		}
		if update.Stdout != nil {
			steps[idx].Stdout = update.Stdout
		}
		if update.Stderr != nil {
			steps[idx].Stderr = update.Stderr
		}
		if update.Status == domain.BuildStepStatusFailed {
			steps[idx].ErrorMessage = update.ErrorMessage
		} else {
			steps[idx].ErrorMessage = nil
		}
		if update.StartedAt != nil {
			steps[idx].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			steps[idx].FinishedAt = update.FinishedAt
		}

		r.buildSteps[buildID] = steps
		return cloneStep(steps[idx]), true, nil
	}

	return domain.BuildStep{}, false, nil
}

func (r *BuildRepository) CompleteStep(_ context.Context, request repository.CompleteStepRequest) (repository.CompleteStepResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	build, ok := r.builds[request.BuildID]
	if !ok {
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, repository.ErrBuildNotFound
	}

	steps := r.buildSteps[request.BuildID]
	for idx := range steps {
		if steps[idx].StepIndex != request.StepIndex {
			continue
		}

		claimMatches := true
		if request.RequireClaim {
			claimMatches = steps[idx].ClaimToken != nil && *steps[idx].ClaimToken == request.ClaimToken
		}

		completion := repository.DecideStepCompletion(steps[idx].Status, request.Update.Status, request.RequireClaim, claimMatches)
		if !completion.AllowUpdate {
			if completion.Outcome == repository.StepCompletionDuplicateTerminal {
				return repository.CompleteStepResult{Step: cloneStep(steps[idx]), Outcome: completion.Outcome}, nil
			}
			if completion.Outcome == repository.StepCompletionStaleClaim {
				return repository.CompleteStepResult{Step: cloneStep(steps[idx]), Outcome: completion.Outcome}, nil
			}
			return repository.CompleteStepResult{Outcome: completion.Outcome}, nil
		}

		if build.Status != domain.BuildStatusRunning {
			return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
		}

		steps[idx].Status = request.Update.Status
		if request.Update.WorkerID != nil {
			steps[idx].WorkerID = request.Update.WorkerID
		}
		if request.Update.ExitCode != nil {
			steps[idx].ExitCode = request.Update.ExitCode
		}
		if request.Update.Stdout != nil {
			steps[idx].Stdout = request.Update.Stdout
		}
		if request.Update.Stderr != nil {
			steps[idx].Stderr = request.Update.Stderr
		}
		if request.Update.Status == domain.BuildStepStatusFailed {
			steps[idx].ErrorMessage = request.Update.ErrorMessage
		} else {
			steps[idx].ErrorMessage = nil
		}
		if request.Update.StartedAt != nil {
			steps[idx].StartedAt = request.Update.StartedAt
		}
		if request.Update.FinishedAt != nil {
			steps[idx].FinishedAt = request.Update.FinishedAt
		}
		if request.Update.Status == domain.BuildStepStatusSuccess || request.Update.Status == domain.BuildStepStatusFailed {
			steps[idx].ClaimToken = nil
			steps[idx].ClaimedAt = nil
			steps[idx].LeaseExpiresAt = nil
		}

		now := time.Now().UTC()
		hasNext := false
		for scanIdx := range steps {
			if steps[scanIdx].StepIndex > request.StepIndex {
				hasNext = true
				break
			}
		}

		advance := repository.DecideBuildAdvancement(request.Update.Status, request.StepIndex, hasNext)
		if advance.FailBuild {
			if !domain.CanTransitionBuild(build.Status, domain.BuildStatusFailed) {
				return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
			}
			build.Status = domain.BuildStatusFailed
			build.FinishedAt = &now
			build.ErrorMessage = steps[idx].ErrorMessage
		} else if advance.SucceedBuild {
			if !domain.CanTransitionBuild(build.Status, domain.BuildStatusSuccess) {
				return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
			}
			if advance.NextStepIndex > build.CurrentStepIndex {
				build.CurrentStepIndex = advance.NextStepIndex
			}
			build.Status = domain.BuildStatusSuccess
			build.FinishedAt = &now
			build.ErrorMessage = nil
		} else if advance.AdvanceCurrentStepIndex && advance.NextStepIndex > build.CurrentStepIndex {
			build.CurrentStepIndex = advance.NextStepIndex
		}

		r.builds[request.BuildID] = build
		r.buildSteps[request.BuildID] = steps
		return repository.CompleteStepResult{Step: cloneStep(steps[idx]), Outcome: repository.StepCompletionCompleted}, nil
	}

	return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, repository.ErrBuildNotFound
}

func (r *BuildRepository) UpdateCurrentStepIndex(_ context.Context, id string, currentStepIndex int) (domain.Build, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	build.CurrentStepIndex = currentStepIndex
	r.builds[id] = build

	return build, nil
}

func cloneStep(step domain.BuildStep) domain.BuildStep {
	if step.Args != nil {
		step.Args = append([]string(nil), step.Args...)
	}
	if step.Env != nil {
		env := make(map[string]string, len(step.Env))
		for key, value := range step.Env {
			env[key] = value
		}
		step.Env = env
	}
	if step.ArtifactPaths != nil {
		step.ArtifactPaths = append([]string(nil), step.ArtifactPaths...)
	}

	return step
}

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
