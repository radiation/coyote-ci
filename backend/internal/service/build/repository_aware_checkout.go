package build

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

var ErrRepositoryCheckoutIdentityIncomplete = errors.New("repository checkout identity is incomplete")
var ErrRepositoryCheckoutIdentityMismatch = errors.New("repository checkout identity does not match registered repository")
var ErrRepositoryCheckoutConnectionDisabled = errors.New("scm connection is disabled for checkout")
var ErrRepositoryCheckoutRepositoryDisabled = errors.New("registered repository is disabled for checkout")
var ErrRepositoryCheckoutConnectionInvalid = errors.New("github app connection is invalid for checkout")
var ErrRepositoryCheckoutPrivateKeyUnavailable = errors.New("github app private key is unavailable for checkout")
var ErrRepositoryCheckoutRepositoryUnavailable = errors.New("repository is unavailable through the snapshotted connection")

type repositoryCheckoutConnectionRepository interface {
	GetByID(ctx context.Context, id string) (domain.SCMConnectionDetail, error)
}

type repositoryCheckoutRegistrationRepository interface {
	GetByID(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error)
}

type repositoryCheckoutSecretResolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

type repositoryCheckoutGitHubClient interface {
	GetInstallationToken(ctx context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error)
	GetFreshInstallationToken(ctx context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error)
	GetRepositoryByID(ctx context.Context, input platformgithubapp.InstallationTokenRequest, repositoryID string) (platformgithubapp.Repository, error)
}

type RepositoryAwareCheckoutResolverConfig struct {
	Connections   repositoryCheckoutConnectionRepository
	Registrations repositoryCheckoutRegistrationRepository
	Secrets       repositoryCheckoutSecretResolver
	GitHub        repositoryCheckoutGitHubClient
}

type RepositoryAwareCheckoutResolver struct {
	connections   repositoryCheckoutConnectionRepository
	registrations repositoryCheckoutRegistrationRepository
	secrets       repositoryCheckoutSecretResolver
	github        repositoryCheckoutGitHubClient
}

type RepositoryAwareCheckout struct {
	RepositoryURL     string
	Credential        source.HTTPSCredential
	refreshCredential func(context.Context) (source.HTTPSCredential, error)
}

func NewRepositoryAwareCheckoutResolver(cfg RepositoryAwareCheckoutResolverConfig) (*RepositoryAwareCheckoutResolver, error) {
	if cfg.Connections == nil || cfg.Registrations == nil || cfg.Secrets == nil || cfg.GitHub == nil {
		return nil, errors.New("repository-aware checkout resolver requires connection, registered repository, secret, and github app dependencies")
	}
	return &RepositoryAwareCheckoutResolver{connections: cfg.Connections, registrations: cfg.Registrations, secrets: cfg.Secrets, github: cfg.GitHub}, nil
}

func (r *RepositoryAwareCheckoutResolver) Resolve(ctx context.Context, snapshot domain.RepositoryIdentitySnapshot) (RepositoryAwareCheckout, error) {
	if err := snapshot.Validate(); err != nil {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutIdentityIncomplete
	}
	registration, err := r.registrations.GetByID(ctx, snapshot.RegisteredRepositoryID)
	if err != nil {
		return RepositoryAwareCheckout{}, fmt.Errorf("registered repository: %w", err)
	}
	if registration.ID != snapshot.RegisteredRepositoryID || registration.ConnectionID != snapshot.SCMConnectionID || registration.ProviderRepositoryID != snapshot.ProviderRepositoryID {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutIdentityMismatch
	}
	if registration.Disabled {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutRepositoryDisabled
	}
	detail, err := r.connections.GetByID(ctx, snapshot.SCMConnectionID)
	if err != nil {
		return RepositoryAwareCheckout{}, fmt.Errorf("scm connection: %w", err)
	}
	if !detail.Connection.Enabled {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutConnectionDisabled
	}
	if detail.Connection.Provider != domain.SCMProviderGitHub || detail.GitHubAppRegistration == nil || detail.GitHubAppInstallation == nil {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutConnectionInvalid
	}
	app := detail.GitHubAppRegistration.Normalize()
	installation := detail.GitHubAppInstallation.Normalize()
	if app.ID == "" || app.AppID == "" || app.PrivateKeySecretRef == "" || app.APIBaseURL == "" || installation.InstallationID == "" || detail.Connection.APIBaseURL != app.APIBaseURL {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutConnectionInvalid
	}
	privateKey, err := r.secrets.Resolve(ctx, app.PrivateKeySecretRef)
	if err != nil || strings.TrimSpace(privateKey) == "" {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutPrivateKeyUnavailable
	}
	request := platformgithubapp.InstallationTokenRequest{AppRegistrationID: app.ID, AppID: app.AppID, InstallationID: installation.InstallationID, APIBaseURL: app.APIBaseURL, PrivateKeyPEM: privateKey, RepositoryIDs: []string{snapshot.ProviderRepositoryID}}
	providerRepository, err := r.github.GetRepositoryByID(ctx, request, snapshot.ProviderRepositoryID)
	if err != nil {
		return RepositoryAwareCheckout{}, classifyRepositoryCheckoutProviderError(err)
	}
	if strings.TrimSpace(providerRepository.ID) != snapshot.ProviderRepositoryID || strings.TrimSpace(providerRepository.CloneURL) == "" {
		return RepositoryAwareCheckout{}, ErrRepositoryCheckoutIdentityMismatch
	}
	token, err := r.github.GetInstallationToken(ctx, request)
	if err != nil {
		return RepositoryAwareCheckout{}, classifyRepositoryCheckoutProviderError(err)
	}
	return RepositoryAwareCheckout{
		RepositoryURL: strings.TrimSpace(providerRepository.CloneURL),
		Credential:    source.HTTPSCredential{Username: "x-access-token", Password: token.Value},
		refreshCredential: func(refreshCtx context.Context) (source.HTTPSCredential, error) {
			freshToken, refreshErr := r.github.GetFreshInstallationToken(refreshCtx, request)
			if refreshErr != nil {
				return source.HTTPSCredential{}, classifyRepositoryCheckoutProviderError(refreshErr)
			}
			return source.HTTPSCredential{Username: "x-access-token", Password: freshToken.Value}, nil
		},
	}, nil
}

// RunWithCredentialRetry invokes operation once and retries exactly once with a
// freshly exchanged credential only after an explicit authentication rejection.
func (checkout RepositoryAwareCheckout) RunWithCredentialRetry(ctx context.Context, operation func(source.HTTPSCredential) error) error {
	if operation == nil {
		return errors.New("repository checkout operation is required")
	}
	operationErr := operation(checkout.Credential)
	if !source.IsAuthenticationFailure(operationErr) || checkout.refreshCredential == nil {
		return operationErr
	}
	freshCredential, refreshErr := checkout.refreshCredential(ctx)
	if refreshErr != nil {
		return refreshErr
	}
	return operation(freshCredential)
}

func classifyRepositoryCheckoutProviderError(err error) error {
	switch {
	case errors.Is(err, platformgithubapp.ErrRepositoryInaccessible), errors.Is(err, platformgithubapp.ErrInstallationUnavailable):
		return ErrRepositoryCheckoutRepositoryUnavailable
	case errors.Is(err, platformgithubapp.ErrPrivateKeyMissing), errors.Is(err, platformgithubapp.ErrPrivateKeyMalformed), errors.Is(err, platformgithubapp.ErrPrivateKeyNotRSA):
		return ErrRepositoryCheckoutPrivateKeyUnavailable
	default:
		return err
	}
}
