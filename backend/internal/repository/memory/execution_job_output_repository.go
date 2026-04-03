package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type ExecutionJobOutputRepository struct {
	mu             sync.RWMutex
	outputsByID    map[string]domain.ExecutionJobOutput
	outputsByBuild map[string][]string
	outputsByJob   map[string][]string
}

func NewExecutionJobOutputRepository() *ExecutionJobOutputRepository {
	return &ExecutionJobOutputRepository{
		outputsByID:    map[string]domain.ExecutionJobOutput{},
		outputsByBuild: map[string][]string{},
		outputsByJob:   map[string][]string{},
	}
}

func (r *ExecutionJobOutputRepository) CreateMany(_ context.Context, outputs []domain.ExecutionJobOutput) ([]domain.ExecutionJobOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]domain.ExecutionJobOutput, 0, len(outputs))
	for _, output := range outputs {
		r.outputsByID[output.ID] = output
		r.outputsByBuild[output.BuildID] = append(r.outputsByBuild[output.BuildID], output.ID)
		r.outputsByJob[output.JobID] = append(r.outputsByJob[output.JobID], output.ID)
		out = append(out, output)
	}
	return out, nil
}

func (r *ExecutionJobOutputRepository) ListByBuildID(_ context.Context, buildID string) ([]domain.ExecutionJobOutput, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.collectOrdered(r.outputsByBuild[buildID]), nil
}

func (r *ExecutionJobOutputRepository) ListByJobID(_ context.Context, jobID string) ([]domain.ExecutionJobOutput, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.collectOrdered(r.outputsByJob[jobID]), nil
}

func (r *ExecutionJobOutputRepository) collectOrdered(ids []string) []domain.ExecutionJobOutput {
	out := make([]domain.ExecutionJobOutput, 0, len(ids))
	for _, id := range ids {
		if value, ok := r.outputsByID[id]; ok {
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}
