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
	identities  repository.UserSlackIdentityRepository
}

func NewSlackWorkspaceIntegrationRepository() *SlackWorkspaceIntegrationRepository {
	return &SlackWorkspaceIntegrationRepository{}
}

func (r *SlackWorkspaceIntegrationRepository) SetUserSlackIdentityRepository(identities repository.UserSlackIdentityRepository) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.identities = identities
}

func (r *SlackWorkspaceIntegrationRepository) Get(ctx context.Context) (domain.SlackWorkspaceIntegration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.integration == nil {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	copy := *r.integration
	count, err := r.linkedIdentityCountLocked(ctx, copy.ID)
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, err
	}
	copy.LinkedIdentityCount = count
	return copy, nil
}

func (r *SlackWorkspaceIntegrationRepository) ConnectOrReplace(ctx context.Context, candidate domain.SlackWorkspaceIntegration, replaceDifferentWorkspace bool) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.integration == nil {
		copy := candidate
		r.integration = &copy
		return copy, nil
	}

	existing := *r.integration
	linkedCount, err := r.linkedIdentityCountLocked(ctx, existing.ID)
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, err
	}
	if existing.WorkspaceID != candidate.WorkspaceID && linkedCount > 0 {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationLinkedIdentitiesExist
	}
	if existing.WorkspaceID != candidate.WorkspaceID && !replaceDifferentWorkspace {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationReplaceRequired
	}

	candidate.ID = existing.ID
	candidate.Enabled = existing.Enabled
	candidate.CreatedAt = existing.CreatedAt
	copy := candidate
	copy.LinkedIdentityCount = linkedCount
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

func (r *SlackWorkspaceIntegrationRepository) Delete(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.integration == nil {
		return repository.ErrSlackWorkspaceIntegrationNotFound
	}
	linkedCount, err := r.linkedIdentityCountLocked(ctx, r.integration.ID)
	if err != nil {
		return err
	}
	if linkedCount > 0 {
		return repository.ErrSlackWorkspaceIntegrationLinkedIdentitiesExist
	}
	r.integration = nil
	return nil
}

func (r *SlackWorkspaceIntegrationRepository) linkedIdentityCountLocked(ctx context.Context, workspaceIntegrationID string) (int, error) {
	if r.identities == nil {
		return 0, nil
	}
	return r.identities.CountByWorkspaceIntegrationID(ctx, workspaceIntegrationID)
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}
