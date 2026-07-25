package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
	platformsecret "github.com/radiation/coyote-ci/backend/internal/platform/secret"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrSCMConnectionDisplayNameRequired = errors.New("display_name is required")
var ErrSCMGitHubAppRegistrationIDRequired = errors.New("github installation app_registration_id is required")
var ErrSCMGitHubAppIDRequired = errors.New("github app app_id is required")
var ErrSCMGitHubPrivateKeySecretRefRequired = errors.New("github app private_key_secret_ref is required")
var ErrSCMGitHubWebhookSecretRefRequired = errors.New("github app webhook_secret_ref is required")
var ErrSCMGitHubInstallationIDRequired = errors.New("github installation installation_id is required")
var ErrSCMGitHubAccountLoginRequired = errors.New("github installation account_login is required")
var ErrSCMGitHubAccountTypeRequired = errors.New("github installation account_type is required")
var ErrSCMGitHubTargetIDRequired = errors.New("github installation target_id is required")
var ErrSCMConnectionEnabledRequired = errors.New("enabled is required")
var ErrSCMConnectionDisabled = errors.New("scm connection is disabled")
var ErrSCMRegisteredRepositoryConnectionIDRequired = errors.New("connection_id is required")
var ErrSCMRegisteredRepositorySelectorInvalid = errors.New("exactly one repository selector is required: provider_repository_id or owner plus name")
var ErrSCMGitHubConnectionConfigurationInvalid = errors.New("github app connection configuration is invalid")
var ErrSCMGitHubPrivateKeyResolveFailed = errors.New("github app private key could not be resolved")
var ErrSCMGitHubAuthenticationFailed = errors.New("github app authentication failed")
var ErrSCMGitHubInstallationUnavailable = errors.New("github app installation is unavailable")
var ErrSCMGitHubRepositoryNotAccessible = errors.New("github repository is not accessible through the selected connection")
var ErrSCMGitHubRateLimited = errors.New("github app request was rate limited")
var ErrSCMGitHubProviderUnavailable = errors.New("github app provider is unavailable")
var ErrSCMGitHubProviderMalformedResponse = errors.New("github app provider response was malformed")

type scmSecretResolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

type scmGitHubAppClient interface {
	GetInstallationToken(ctx context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error)
	ProbeInstallation(ctx context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationProbeResult, error)
}

type scmGitHubRepositoryResolver interface {
	GetRepositoryByID(ctx context.Context, input platformgithubapp.InstallationTokenRequest, repositoryID string) (platformgithubapp.Repository, error)
	GetRepositoryByOwnerAndName(ctx context.Context, input platformgithubapp.InstallationTokenRequest, owner string, name string) (platformgithubapp.Repository, error)
}

type SCMAdminService struct {
	connections  repository.SCMConnectionRepository
	repositories repository.SCMRepositoryRegistrationRepository
	secrets      scmSecretResolver
	githubApps   scmGitHubAppClient
	githubRepos  scmGitHubRepositoryResolver
	now          func() time.Time
}

type CreateGitHubAppInstallationConnectionInput struct {
	AppRegistrationID string
	DisplayName       string
	Enabled           *bool
	InstallationID    string
	AccountLogin      string
	AccountType       string
	TargetID          string
}

type CreateGitHubAppRegistrationInput struct {
	AppID               string
	DisplayName         *string
	APIBaseURL          string
	WebBaseURL          string
	PrivateKeySecretRef string
	WebhookSecretRef    string
}

type CreateSCMRepositoryRegistrationInput struct {
	ConnectionID         string
	ProviderRepositoryID string
	Owner                string
	Name                 string
}

func NewSCMAdminService(connections repository.SCMConnectionRepository, repositories repository.SCMRepositoryRegistrationRepository) *SCMAdminService {
	githubClient := platformgithubapp.NewClient(nil)
	return &SCMAdminService{
		connections:  connections,
		repositories: repositories,
		secrets:      platformsecret.NewEnvResolver(),
		githubApps:   githubClient,
		githubRepos:  githubClient,
		now:          time.Now,
	}
}

func (s *SCMAdminService) ListConnections(ctx context.Context) ([]domain.SCMConnectionDetail, error) {
	return s.connections.List(ctx)
}

func (s *SCMAdminService) GetConnection(ctx context.Context, id string) (domain.SCMConnectionDetail, error) {
	return s.connections.GetByID(ctx, strings.TrimSpace(id))
}

func (s *SCMAdminService) ListGitHubAppRegistrations(ctx context.Context) ([]domain.GitHubAppRegistration, error) {
	return s.connections.ListGitHubAppRegistrations(ctx)
}

func (s *SCMAdminService) GetGitHubAppRegistration(ctx context.Context, id string) (domain.GitHubAppRegistration, error) {
	return s.connections.GetGitHubAppRegistrationByID(ctx, strings.TrimSpace(id))
}

func (s *SCMAdminService) CreateGitHubAppRegistration(ctx context.Context, input CreateGitHubAppRegistrationInput) (domain.GitHubAppRegistration, error) {
	if strings.TrimSpace(input.AppID) == "" {
		return domain.GitHubAppRegistration{}, ErrSCMGitHubAppIDRequired
	}
	if strings.TrimSpace(input.PrivateKeySecretRef) == "" {
		return domain.GitHubAppRegistration{}, ErrSCMGitHubPrivateKeySecretRefRequired
	}
	if strings.TrimSpace(input.WebhookSecretRef) == "" {
		return domain.GitHubAppRegistration{}, ErrSCMGitHubWebhookSecretRefRequired
	}

	now := s.now().UTC()
	registration := domain.GitHubAppRegistration{
		ID:                  uuid.NewString(),
		AppID:               strings.TrimSpace(input.AppID),
		DisplayName:         input.DisplayName,
		APIBaseURL:          input.APIBaseURL,
		WebBaseURL:          input.WebBaseURL,
		PrivateKeySecretRef: strings.TrimSpace(input.PrivateKeySecretRef),
		WebhookSecretRef:    strings.TrimSpace(input.WebhookSecretRef),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	return s.connections.CreateGitHubAppRegistration(ctx, registration)
}

func (s *SCMAdminService) CreateGitHubAppInstallationConnection(ctx context.Context, input CreateGitHubAppInstallationConnectionInput) (domain.SCMConnectionDetail, error) {
	if strings.TrimSpace(input.AppRegistrationID) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubAppRegistrationIDRequired
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return domain.SCMConnectionDetail{}, ErrSCMConnectionDisplayNameRequired
	}
	if input.Enabled == nil {
		return domain.SCMConnectionDetail{}, ErrSCMConnectionEnabledRequired
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
	if strings.TrimSpace(input.TargetID) == "" {
		return domain.SCMConnectionDetail{}, ErrSCMGitHubTargetIDRequired
	}
	registration, err := s.connections.GetGitHubAppRegistrationByID(ctx, strings.TrimSpace(input.AppRegistrationID))
	if err != nil {
		return domain.SCMConnectionDetail{}, err
	}

	now := s.now().UTC()
	connectionID := uuid.NewString()
	deploymentKind := inferGitHubDeploymentKind(registration.APIBaseURL, registration.WebBaseURL)
	detail := domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: connectionID, Provider: domain.SCMProviderGitHub, DisplayName: displayName, DeploymentKind: deploymentKind, APIBaseURL: registration.APIBaseURL, WebBaseURL: registration.WebBaseURL, Enabled: *input.Enabled, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &registration,
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: connectionID, AppRegistrationID: registration.ID, InstallationID: strings.TrimSpace(input.InstallationID), AccountLogin: strings.TrimSpace(input.AccountLogin), AccountType: strings.TrimSpace(input.AccountType), AccountID: strings.TrimSpace(input.TargetID), CreatedAt: now, UpdatedAt: now},
	}
	return s.connections.CreateGitHubAppInstallationConnection(ctx, detail)
}

func (s *SCMAdminService) SetConnectionEnabled(ctx context.Context, id string, enabled *bool) (domain.SCMConnectionDetail, error) {
	if enabled == nil {
		return domain.SCMConnectionDetail{}, ErrSCMConnectionEnabledRequired
	}
	return s.connections.SetEnabled(ctx, strings.TrimSpace(id), *enabled, s.now().UTC())
}

func (s *SCMAdminService) GetGitHubAppInstallationToken(ctx context.Context, id string) (string, error) {
	request, _, err := s.resolveGitHubInstallationTokenRequest(ctx, strings.TrimSpace(id))
	if err != nil {
		return "", err
	}
	token, err := s.githubApps.GetInstallationToken(ctx, request)
	if err != nil {
		return "", mapSCMGitHubProviderError(err)
	}
	return token.Value, nil
}

func (s *SCMAdminService) TestConnection(ctx context.Context, id string) (domain.SCMConnectionDetail, error) {
	trimmedID := strings.TrimSpace(id)
	request, detail, err := s.resolveGitHubInstallationTokenRequest(ctx, trimmedID)
	if err != nil {
		if isContextCancellation(err) {
			return domain.SCMConnectionDetail{}, err
		}
		if persisted, persistErr := s.persistConnectionHealthFailure(ctx, trimmedID, err); persistErr == nil {
			return persisted, err
		}
		return domain.SCMConnectionDetail{}, err
	}
	_, err = s.githubApps.ProbeInstallation(ctx, request)
	if err != nil {
		mappedErr := mapSCMGitHubProviderError(err)
		if isContextCancellation(mappedErr) {
			return domain.SCMConnectionDetail{}, mappedErr
		}
		persisted, persistErr := s.persistConnectionHealthFailure(ctx, detail.Connection.ID, mappedErr)
		if persistErr == nil {
			return persisted, mappedErr
		}
		return domain.SCMConnectionDetail{}, mappedErr
	}
	now := s.now().UTC()
	return s.connections.UpdateHealth(ctx, detail.Connection.ID, domain.SCMConnectionHealthStatusHealthy, stringPtr("github app installation authentication succeeded"), now, now)
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
	selector, err := normalizeRepositorySelector(input)
	if err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	request, _, err := s.resolveGitHubInstallationTokenRequest(ctx, strings.TrimSpace(input.ConnectionID))
	if err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	resolvedRepository, err := s.resolveGitHubRepository(ctx, request, selector)
	if err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	now := s.now().UTC()
	registration := mapGitHubRepositoryRegistration(uuid.NewString(), strings.TrimSpace(input.ConnectionID), resolvedRepository, now, now, now)
	return s.repositories.Create(ctx, registration)
}

func (s *SCMAdminService) RefreshRegisteredRepository(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error) {
	registration, err := s.repositories.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	request, _, err := s.resolveGitHubInstallationTokenRequest(ctx, registration.ConnectionID)
	if err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	resolvedRepository, err := s.githubRepos.GetRepositoryByID(ctx, request, registration.ProviderRepositoryID)
	if err != nil {
		return domain.SCMRepositoryRegistration{}, mapSCMGitHubProviderError(err)
	}
	if resolvedRepository.ID != registration.ProviderRepositoryID {
		return domain.SCMRepositoryRegistration{}, ErrSCMGitHubProviderMalformedResponse
	}
	now := s.now().UTC()
	updated := mapGitHubRepositoryRegistration(registration.ID, registration.ConnectionID, resolvedRepository, registration.CreatedAt, now, now)
	return s.repositories.Update(ctx, updated)
}

type scmRepositorySelector struct {
	providerRepositoryID string
	owner                string
	name                 string
}

func normalizeRepositorySelector(input CreateSCMRepositoryRegistrationInput) (scmRepositorySelector, error) {
	selector := scmRepositorySelector{
		providerRepositoryID: strings.TrimSpace(input.ProviderRepositoryID),
		owner:                strings.TrimSpace(input.Owner),
		name:                 strings.TrimSpace(input.Name),
	}
	hasProviderID := selector.providerRepositoryID != ""
	hasOwner := selector.owner != ""
	hasName := selector.name != ""
	if hasProviderID {
		if hasOwner || hasName {
			return scmRepositorySelector{}, ErrSCMRegisteredRepositorySelectorInvalid
		}
		return selector, nil
	}
	if hasOwner && hasName {
		return selector, nil
	}
	return scmRepositorySelector{}, ErrSCMRegisteredRepositorySelectorInvalid
}

func (s *SCMAdminService) resolveGitHubRepository(ctx context.Context, request platformgithubapp.InstallationTokenRequest, selector scmRepositorySelector) (platformgithubapp.Repository, error) {
	if selector.providerRepositoryID != "" {
		repository, err := s.githubRepos.GetRepositoryByID(ctx, request, selector.providerRepositoryID)
		if err != nil {
			return platformgithubapp.Repository{}, mapSCMGitHubProviderError(err)
		}
		if repository.ID != selector.providerRepositoryID {
			return platformgithubapp.Repository{}, ErrSCMGitHubProviderMalformedResponse
		}
		return repository, nil
	}
	repository, err := s.githubRepos.GetRepositoryByOwnerAndName(ctx, request, selector.owner, selector.name)
	if err != nil {
		return platformgithubapp.Repository{}, mapSCMGitHubProviderError(err)
	}
	return repository, nil
}

func mapGitHubRepositoryRegistration(id string, connectionID string, repository platformgithubapp.Repository, createdAt time.Time, metadataRefreshedAt time.Time, updatedAt time.Time) domain.SCMRepositoryRegistration {
	return domain.SCMRepositoryRegistration{
		ID:                   strings.TrimSpace(id),
		ConnectionID:         strings.TrimSpace(connectionID),
		ProviderRepositoryID: strings.TrimSpace(repository.ID),
		Owner:                strings.TrimSpace(repository.Owner),
		Name:                 strings.TrimSpace(repository.Name),
		FullName:             strings.TrimSpace(repository.FullName),
		CloneURL:             strings.TrimSpace(repository.CloneURL),
		WebURL:               strings.TrimSpace(repository.WebURL),
		DefaultBranch:        repository.DefaultBranch,
		Archived:             repository.Archived,
		Disabled:             repository.Disabled,
		MetadataRefreshedAt:  metadataRefreshedAt.UTC(),
		CreatedAt:            createdAt.UTC(),
		UpdatedAt:            updatedAt.UTC(),
	}
}

func inferGitHubDeploymentKind(apiBaseURL string, webBaseURL string) domain.SCMDeploymentKind {
	if strings.TrimSpace(apiBaseURL) == "https://api.github.com" && strings.TrimSpace(webBaseURL) == "https://github.com" {
		return domain.SCMDeploymentKindCloud
	}
	return domain.SCMDeploymentKindSelfHosted
}

func (s *SCMAdminService) resolveGitHubInstallationTokenRequest(ctx context.Context, id string) (platformgithubapp.InstallationTokenRequest, domain.SCMConnectionDetail, error) {
	detail, err := s.connections.GetByID(ctx, id)
	if err != nil {
		return platformgithubapp.InstallationTokenRequest{}, domain.SCMConnectionDetail{}, err
	}
	if !detail.Connection.Enabled {
		return platformgithubapp.InstallationTokenRequest{}, detail, ErrSCMConnectionDisabled
	}
	if detail.Connection.Provider != domain.SCMProviderGitHub || detail.GitHubAppRegistration == nil || detail.GitHubAppInstallation == nil {
		return platformgithubapp.InstallationTokenRequest{}, detail, ErrSCMGitHubConnectionConfigurationInvalid
	}
	registration := detail.GitHubAppRegistration.Normalize()
	installation := detail.GitHubAppInstallation.Normalize()
	if detail.Connection.APIBaseURL != registration.APIBaseURL || detail.Connection.WebBaseURL != registration.WebBaseURL {
		return platformgithubapp.InstallationTokenRequest{}, detail, ErrSCMGitHubConnectionConfigurationInvalid
	}
	if registration.PrivateKeySecretRef == "" || registration.AppID == "" || installation.InstallationID == "" {
		return platformgithubapp.InstallationTokenRequest{}, detail, ErrSCMGitHubConnectionConfigurationInvalid
	}
	privateKey, resolveErr := s.secrets.Resolve(ctx, registration.PrivateKeySecretRef)
	if resolveErr != nil || strings.TrimSpace(privateKey) == "" {
		return platformgithubapp.InstallationTokenRequest{}, detail, ErrSCMGitHubPrivateKeyResolveFailed
	}
	return platformgithubapp.InstallationTokenRequest{
		AppRegistrationID: registration.ID,
		AppID:             registration.AppID,
		InstallationID:    installation.InstallationID,
		APIBaseURL:        registration.APIBaseURL,
		PrivateKeyPEM:     privateKey,
	}, detail, nil
}

func (s *SCMAdminService) persistConnectionHealthFailure(ctx context.Context, id string, err error) (domain.SCMConnectionDetail, error) {
	status, summary := scmConnectionHealthFailure(err)
	now := s.now().UTC()
	return s.connections.UpdateHealth(ctx, id, status, stringPtr(summary), now, now)
}

func scmConnectionHealthFailure(err error) (domain.SCMConnectionHealthStatus, string) {
	switch {
	case errors.Is(err, ErrSCMConnectionDisabled), errors.Is(err, ErrSCMGitHubConnectionConfigurationInvalid), errors.Is(err, ErrSCMGitHubPrivateKeyResolveFailed), errors.Is(err, ErrSCMGitHubAuthenticationFailed):
		return domain.SCMConnectionHealthStatusUnhealthy, err.Error()
	case errors.Is(err, ErrSCMGitHubInstallationUnavailable):
		return domain.SCMConnectionHealthStatusRevoked, err.Error()
	case errors.Is(err, ErrSCMGitHubRateLimited), errors.Is(err, ErrSCMGitHubProviderUnavailable), errors.Is(err, ErrSCMGitHubProviderMalformedResponse):
		return domain.SCMConnectionHealthStatusDegraded, err.Error()
	default:
		return domain.SCMConnectionHealthStatusUnhealthy, ErrSCMGitHubProviderUnavailable.Error()
	}
}

func mapSCMGitHubProviderError(err error) error {
	switch {
	case errors.Is(err, platformgithubapp.ErrPrivateKeyMissing), errors.Is(err, platformgithubapp.ErrPrivateKeyMalformed), errors.Is(err, platformgithubapp.ErrPrivateKeyNotRSA):
		return ErrSCMGitHubPrivateKeyResolveFailed
	case errors.Is(err, platformgithubapp.ErrAuthentication):
		return ErrSCMGitHubAuthenticationFailed
	case errors.Is(err, platformgithubapp.ErrInstallationUnavailable):
		return ErrSCMGitHubInstallationUnavailable
	case errors.Is(err, platformgithubapp.ErrRepositoryInaccessible):
		return ErrSCMGitHubRepositoryNotAccessible
	case errors.Is(err, platformgithubapp.ErrRateLimited):
		return ErrSCMGitHubRateLimited
	case errors.Is(err, platformgithubapp.ErrMalformedResponse):
		return ErrSCMGitHubProviderMalformedResponse
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return ErrSCMGitHubProviderUnavailable
	}
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func isContextCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
