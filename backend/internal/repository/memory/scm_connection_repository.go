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
	registrationID, err := r.resolveGitHubAppRegistrationLocked(registration)
	if err != nil {
		return domain.SCMConnectionDetail{}, err
	}
	registration = r.githubAppRegistrations[registrationID]

	installation := *detail.GitHubAppInstallation
	installation.AppRegistrationID = registration.ID
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

func (r *SCMConnectionRepository) resolveGitHubAppRegistrationLocked(registration domain.GitHubAppRegistration) (string, error) {
	key := githubAppRegistrationIndexKey(registration.AppID, registration.APIBaseURL, registration.WebBaseURL)
	if existingID, ok := r.registrationIndexByAppHost[key]; ok {
		existing := r.githubAppRegistrations[existingID]
		if existing.PrivateKeySecretRef != registration.PrivateKeySecretRef || existing.WebhookSecretRef != registration.WebhookSecretRef {
			return "", repository.ErrSCMGitHubAppRegistrationConflict
		}
		return existingID, nil
	}
	r.githubAppRegistrations[registration.ID] = registration
	r.registrationIndexByAppHost[key] = registration.ID
	return registration.ID, nil
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
