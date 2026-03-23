package memory

import (
	"context"
	"sort"
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

	r.builds[build.ID] = build
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
		cloned = append(cloned, step)
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
	copy(out, steps)

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

		return steps[idx], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *BuildRepository) UpdateStepByIndex(_ context.Context, buildID string, stepIndex int, status domain.BuildStepStatus, workerID *string, exitCode *int, errorMessage *string, startedAt *time.Time, finishedAt *time.Time) (domain.BuildStep, error) {
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

		steps[idx].Status = status
		if workerID != nil {
			steps[idx].WorkerID = workerID
		}
		if exitCode != nil {
			steps[idx].ExitCode = exitCode
		}
		if errorMessage != nil || status == domain.BuildStepStatusFailed {
			steps[idx].ErrorMessage = errorMessage
		}
		if startedAt != nil {
			steps[idx].StartedAt = startedAt
		}
		if finishedAt != nil {
			steps[idx].FinishedAt = finishedAt
		}

		r.buildSteps[buildID] = steps
		return steps[idx], nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
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
