package memory

import (
	"context"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SlackWorkspaceIntegrationRepository struct {
	mu          sync.RWMutex
	integration *domain.SlackWorkspaceIntegration
}

func NewSlackWorkspaceIntegrationRepository() *SlackWorkspaceIntegrationRepository {
	return &SlackWorkspaceIntegrationRepository{}
}

func (r *SlackWorkspaceIntegrationRepository) Get(_ context.Context) (domain.SlackWorkspaceIntegration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.integration == nil {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	return *r.integration, nil
}

func (r *SlackWorkspaceIntegrationRepository) ConnectOrReplace(_ context.Context, candidate domain.SlackWorkspaceIntegration, replaceDifferentWorkspace bool) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.integration == nil {
		copy := candidate
		r.integration = &copy
		return copy, nil
	}

	existing := *r.integration
	if existing.WorkspaceID != candidate.WorkspaceID && !replaceDifferentWorkspace {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationReplaceRequired
	}

	candidate.ID = existing.ID
	candidate.Enabled = existing.Enabled
	candidate.CreatedAt = existing.CreatedAt
	copy := candidate
	r.integration = &copy
	return copy, nil
}

func (r *SlackWorkspaceIntegrationRepository) SetEnabled(_ context.Context, enabled bool, updatedAt time.Time) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.integration == nil {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	r.integration.Enabled = enabled
	r.integration.UpdatedAt = updatedAt
	copy := *r.integration
	return copy, nil
}

func (r *SlackWorkspaceIntegrationRepository) UpdateLastTestResult(_ context.Context, testedAt time.Time, succeeded bool) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.integration == nil {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	r.integration.LastTestedAt = &testedAt
	r.integration.LastTestSucceeded = boolPtr(succeeded)
	r.integration.UpdatedAt = testedAt
	copy := *r.integration
	return copy, nil
}

func (r *SlackWorkspaceIntegrationRepository) Delete(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.integration == nil {
		return repository.ErrSlackWorkspaceIntegrationNotFound
	}
	r.integration = nil
	return nil
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}
