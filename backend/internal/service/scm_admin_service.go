package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrSCMConnectionDisplayNameRequired = errors.New("display_name is required")
var ErrSCMConnectionDeploymentKindInvalid = errors.New("deployment_kind must be one of cloud, self_hosted")
var ErrSCMGitHubAppIDRequired = errors.New("github app app_id is required")
var ErrSCMGitHubPrivateKeySecretRefRequired = errors.New("github app private_key_secret_ref is required")
var ErrSCMGitHubWebhookSecretRefRequired = errors.New("github app webhook_secret_ref is required")
var ErrSCMGitHubInstallationIDRequired = errors.New("github installation installation_id is required")
var ErrSCMGitHubAccountLoginRequired = errors.New("github installation account_login is required")
var ErrSCMGitHubAccountTypeRequired = errors.New("github installation account_type is required")
var ErrSCMGitHubAccountIDRequired = errors.New("github installation account_id is required")
var ErrSCMConnectionEnabledRequired = errors.New("enabled is required")
var ErrSCMRegisteredRepositoryConnectionIDRequired = errors.New("connection_id is required")
var ErrSCMRegisteredRepositoryProviderRepositoryIDRequired = errors.New("provider_repository_id is required")
var ErrSCMRegisteredRepositoryOwnerRequired = errors.New("owner is required")
var ErrSCMRegisteredRepositoryNameRequired = errors.New("name is required")
var ErrSCMRegisteredRepositoryFullNameRequired = errors.New("full_name is required")
var ErrSCMRegisteredRepositoryCloneURLRequired = errors.New("clone_url is required")
var ErrSCMRegisteredRepositoryWebURLRequired = errors.New("web_url is required")

type SCMAdminService struct {
	connections  repository.SCMConnectionRepository
	repositories repository.SCMRepositoryRegistrationRepository
	now          func() time.Time
}

type CreateGitHubAppInstallationConnectionInput struct {
	DisplayName         string
	DeploymentKind      string
	APIBaseURL          string
	WebBaseURL          string
	AppID               string
	AppDisplayName      *string
	PrivateKeySecretRef string
	WebhookSecretRef    string
	InstallationID      string
	AccountLogin        string
	AccountType         string
	AccountID           string
}

type CreateSCMRepositoryRegistrationInput struct {
	ConnectionID         string
	ProviderRepositoryID string
	Owner                string
	Name                 string
	FullName             string
	CloneURL             string
	WebURL               string
	DefaultBranch        *string
	Archived             bool
	Disabled             bool
	MetadataRefreshedAt  *time.Time
}

func NewSCMAdminService(connections repository.SCMConnectionRepository, repositories repository.SCMRepositoryRegistrationRepository) *SCMAdminService {
	return &SCMAdminService{connections: connections, repositories: repositories, now: time.Now}
}

func (s *SCMAdminService) ListConnections(ctx context.Context) ([]domain.SCMConnectionDetail, error) {
	return s.connections.List(ctx)
}

func (s *SCMAdminService) GetConnection(ctx context.Context, id string) (domain.SCMConnectionDetail, error) {
	return s.connections.GetByID(ctx, strings.TrimSpace(id))
}

func (s *SCMAdminService) CreateGitHubAppInstallationConnection(ctx context.Context, input CreateGitHubAppInstallationConnectionInput) (domain.SCMConnectionDetail, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return domain.SCMConnectionDetail{}, ErrSCMConnectionDisplayNameRequired
	}
	deploymentKind := domain.SCMDeploymentKind(strings.TrimSpace(input.DeploymentKind))
	if !deploymentKind.IsValid() {
		return domain.SCMConnectionDetail{}, ErrSCMConnectionDeploymentKindInvalid
	}
	if strings.TrimSpace(input.AppID) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubAppIDRequired
	}
	if strings.TrimSpace(input.PrivateKeySecretRef) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubPrivateKeySecretRefRequired
	}
	if strings.TrimSpace(input.WebhookSecretRef) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubWebhookSecretRefRequired
	}
	if strings.TrimSpace(input.InstallationID) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubInstallationIDRequired
	}
	if strings.TrimSpace(input.AccountLogin) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubAccountLoginRequired
	}
	if strings.TrimSpace(input.AccountType) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubAccountTypeRequired
	}
	if strings.TrimSpace(input.AccountID) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubAccountIDRequired
	}

	now := s.now().UTC()
	connectionID := uuid.NewString()
	registrationID := uuid.NewString()
	detail := domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: connectionID, Provider: domain.SCMProviderGitHub, DisplayName: displayName, DeploymentKind: deploymentKind, APIBaseURL: input.APIBaseURL, WebBaseURL: input.WebBaseURL, Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &domain.GitHubAppRegistration{ID: registrationID, AppID: strings.TrimSpace(input.AppID), DisplayName: input.AppDisplayName, APIBaseURL: input.APIBaseURL, WebBaseURL: input.WebBaseURL, PrivateKeySecretRef: strings.TrimSpace(input.PrivateKeySecretRef), WebhookSecretRef: strings.TrimSpace(input.WebhookSecretRef), CreatedAt: now, UpdatedAt: now},
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: connectionID, AppRegistrationID: registrationID, InstallationID: strings.TrimSpace(input.InstallationID), AccountLogin: strings.TrimSpace(input.AccountLogin), AccountType: strings.TrimSpace(input.AccountType), AccountID: strings.TrimSpace(input.AccountID), CreatedAt: now, UpdatedAt: now},
	}
	return s.connections.CreateGitHubAppInstallationConnection(ctx, detail)
}

func (s *SCMAdminService) SetConnectionEnabled(ctx context.Context, id string, enabled *bool) (domain.SCMConnectionDetail, error) {
	if enabled == nil {
		return domain.SCMConnectionDetail{}, ErrSCMConnectionEnabledRequired
	}
	return s.connections.SetEnabled(ctx, strings.TrimSpace(id), *enabled, s.now().UTC())
}

func (s *SCMAdminService) ListRegisteredRepositories(ctx context.Context) ([]domain.SCMRepositoryRegistration, error) {
	return s.repositories.List(ctx)
}

func (s *SCMAdminService) GetRegisteredRepository(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error) {
	return s.repositories.GetByID(ctx, strings.TrimSpace(id))
}

func (s *SCMAdminService) CreateRegisteredRepository(ctx context.Context, input CreateSCMRepositoryRegistrationInput) (domain.SCMRepositoryRegistration, error) {
	if strings.TrimSpace(input.ConnectionID) == "" {
		return domain.SCMRepositoryRegistration{}, ErrSCMRegisteredRepositoryConnectionIDRequired
	}
	if strings.TrimSpace(input.ProviderRepositoryID) == "" {
		return domain.SCMRepositoryRegistration{}, ErrSCMRegisteredRepositoryProviderRepositoryIDRequired
	}
	if strings.TrimSpace(input.Owner) == "" {
		return domain.SCMRepositoryRegistration{}, ErrSCMRegisteredRepositoryOwnerRequired
	}
	if strings.TrimSpace(input.Name) == "" {
		return domain.SCMRepositoryRegistration{}, ErrSCMRegisteredRepositoryNameRequired
	}
	if strings.TrimSpace(input.FullName) == "" {
		return domain.SCMRepositoryRegistration{}, ErrSCMRegisteredRepositoryFullNameRequired
	}
	if strings.TrimSpace(input.CloneURL) == "" {
		return domain.SCMRepositoryRegistration{}, ErrSCMRegisteredRepositoryCloneURLRequired
	}
	if strings.TrimSpace(input.WebURL) == "" {
		return domain.SCMRepositoryRegistration{}, ErrSCMRegisteredRepositoryWebURLRequired
	}
	if _, err := s.connections.GetByID(ctx, strings.TrimSpace(input.ConnectionID)); err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	now := s.now().UTC()
	metadataRefreshedAt := now
	if input.MetadataRefreshedAt != nil {
		metadataRefreshedAt = input.MetadataRefreshedAt.UTC()
	}
	registration := domain.SCMRepositoryRegistration{
		ID:                   uuid.NewString(),
		ConnectionID:         strings.TrimSpace(input.ConnectionID),
		ProviderRepositoryID: strings.TrimSpace(input.ProviderRepositoryID),
		Owner:                strings.TrimSpace(input.Owner),
		Name:                 strings.TrimSpace(input.Name),
		FullName:             strings.TrimSpace(input.FullName),
		CloneURL:             strings.TrimSpace(input.CloneURL),
		WebURL:               strings.TrimSpace(input.WebURL),
		DefaultBranch:        input.DefaultBranch,
		Archived:             input.Archived,
		Disabled:             input.Disabled,
		MetadataRefreshedAt:  metadataRefreshedAt,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	return s.repositories.Create(ctx, registration)
}

func (s *SCMAdminService) UpdateRegisteredRepository(ctx context.Context, registration domain.SCMRepositoryRegistration) (domain.SCMRepositoryRegistration, error) {
	registration.UpdatedAt = s.now().UTC()
	return s.repositories.Update(ctx, registration)
}
