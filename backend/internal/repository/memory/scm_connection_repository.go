package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SCMConnectionRepository struct {
	mu                         sync.RWMutex
	connections                map[string]domain.SCMConnection
	githubAppRegistrations     map[string]domain.GitHubAppRegistration
	githubAppInstallations     map[string]domain.GitHubAppInstallation
	registrationIndexByAppHost map[string]string
	installationIndex          map[string]string
}

func NewSCMConnectionRepository() *SCMConnectionRepository {
	return &SCMConnectionRepository{
		connections:                map[string]domain.SCMConnection{},
		githubAppRegistrations:     map[string]domain.GitHubAppRegistration{},
		githubAppInstallations:     map[string]domain.GitHubAppInstallation{},
		registrationIndexByAppHost: map[string]string{},
		installationIndex:          map[string]string{},
	}
}

func (r *SCMConnectionRepository) CreateGitHubAppRegistration(_ context.Context, registration domain.GitHubAppRegistration) (domain.GitHubAppRegistration, error) {
	registration = registration.Normalize()
	if err := registration.Validate(); err != nil {
		return domain.GitHubAppRegistration{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.githubAppRegistrations[registration.ID]; exists {
		return domain.GitHubAppRegistration{}, repository.ErrSCMGitHubAppRegistrationConflict
	}
	key := githubAppRegistrationIndexKey(registration.AppID, registration.APIBaseURL, registration.WebBaseURL)
	if _, exists := r.registrationIndexByAppHost[key]; exists {
		return domain.GitHubAppRegistration{}, repository.ErrSCMGitHubAppRegistrationConflict
	}
	r.githubAppRegistrations[registration.ID] = registration
	r.registrationIndexByAppHost[key] = registration.ID
	return registration, nil
}

func (r *SCMConnectionRepository) ListGitHubAppRegistrations(_ context.Context) ([]domain.GitHubAppRegistration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.GitHubAppRegistration, 0, len(r.githubAppRegistrations))
	for _, registration := range r.githubAppRegistrations {
		items = append(items, registration)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (r *SCMConnectionRepository) GetGitHubAppRegistrationByID(_ context.Context, id string) (domain.GitHubAppRegistration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	registration, ok := r.githubAppRegistrations[id]
	if !ok {
		return domain.GitHubAppRegistration{}, repository.ErrSCMGitHubAppRegistrationNotFound
	}
	return registration, nil
}

func (r *SCMConnectionRepository) CreateGitHubAppInstallationConnection(_ context.Context, detail domain.SCMConnectionDetail) (domain.SCMConnectionDetail, error) {
	detail = detail.Normalize()
	if err := detail.Validate(); err != nil {
		return domain.SCMConnectionDetail{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.connections[detail.Connection.ID]; exists {
		return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionConflict
	}

	registration := *detail.GitHubAppRegistration
	storedRegistration, ok := r.githubAppRegistrations[registration.ID]
	if !ok {
		return domain.SCMConnectionDetail{}, repository.ErrSCMGitHubAppRegistrationNotFound
	}
	registration = storedRegistration

	installation := *detail.GitHubAppInstallation
	installationKey := installationIndexKey(registration.ID, installation.InstallationID)
	if _, exists := r.installationIndex[installationKey]; exists {
		return domain.SCMConnectionDetail{}, repository.ErrSCMGitHubAppInstallationConflict
	}

	connection := detail.Connection
	r.connections[connection.ID] = connection
	r.githubAppInstallations[connection.ID] = installation
	r.installationIndex[installationKey] = connection.ID

	return domain.SCMConnectionDetail{Connection: connection, GitHubAppRegistration: &registration, GitHubAppInstallation: &installation}, nil
}

func (r *SCMConnectionRepository) List(_ context.Context) ([]domain.SCMConnectionDetail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.SCMConnectionDetail, 0, len(r.connections))
	for _, connection := range r.connections {
		items = append(items, r.detailLocked(connection.ID))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Connection.CreatedAt.Equal(items[j].Connection.CreatedAt) {
			return items[i].Connection.ID < items[j].Connection.ID
		}
		return items[i].Connection.CreatedAt.After(items[j].Connection.CreatedAt)
	})
	return items, nil
}

func (r *SCMConnectionRepository) GetByID(_ context.Context, id string) (domain.SCMConnectionDetail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.connections[id]; !ok {
		return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
	}
	return r.detailLocked(id), nil
}

func (r *SCMConnectionRepository) SetEnabled(_ context.Context, id string, enabled bool, updatedAt time.Time) (domain.SCMConnectionDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	connection, ok := r.connections[id]
	if !ok {
		return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
	}
	connection.Enabled = enabled
	connection.UpdatedAt = updatedAt.UTC()
	r.connections[id] = connection
	return r.detailLocked(id), nil
}

func (r *SCMConnectionRepository) UpdateHealth(_ context.Context, id string, status domain.SCMConnectionHealthStatus, summary *string, checkedAt time.Time, updatedAt time.Time) (domain.SCMConnectionDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	connection, ok := r.connections[id]
	if !ok {
		return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
	}
	connection.HealthStatus = status
	connection.HealthSummary = summary
	checkedAtUTC := checkedAt.UTC()
	connection.LastHealthCheckedAt = &checkedAtUTC
	connection.UpdatedAt = updatedAt.UTC()
	r.connections[id] = connection
	return r.detailLocked(id), nil
}

func (r *SCMConnectionRepository) detailLocked(connectionID string) domain.SCMConnectionDetail {
	connection := r.connections[connectionID]
	installation, ok := r.githubAppInstallations[connectionID]
	if !ok {
		return domain.SCMConnectionDetail{Connection: connection}
	}
	registration := r.githubAppRegistrations[installation.AppRegistrationID]
	return domain.SCMConnectionDetail{Connection: connection, GitHubAppRegistration: &registration, GitHubAppInstallation: &installation}
}

func githubAppRegistrationIndexKey(appID string, apiBaseURL string, webBaseURL string) string {
	return appID + "|" + apiBaseURL + "|" + webBaseURL
}

func installationIndexKey(appRegistrationID string, installationID string) string {
	return appRegistrationID + "|" + installationID
}
