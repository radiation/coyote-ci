package build

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
)

type scmStatusConnectionRepository interface {
	GetByID(ctx context.Context, id string) (domain.SCMConnectionDetail, error)
}

type scmStatusRegistrationRepository interface {
	GetByID(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error)
}

type scmStatusSecretResolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

type scmStatusGitHubAppClient interface {
	GetInstallationToken(ctx context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error)
}

type GitHubAppCommitStatusPublisherConfig struct {
	Connections   scmStatusConnectionRepository
	Registrations scmStatusRegistrationRepository
	Secrets       scmStatusSecretResolver
	GitHubApps    scmStatusGitHubAppClient
	HTTPClient    *GitHubCommitStatusClient
}

type GitHubAppCommitStatusPublisher struct {
	connections   scmStatusConnectionRepository
	registrations scmStatusRegistrationRepository
	secrets       scmStatusSecretResolver
	githubApps    scmStatusGitHubAppClient
	httpClient    *GitHubCommitStatusClient
}

func NewGitHubAppCommitStatusPublisher(cfg GitHubAppCommitStatusPublisherConfig) (*GitHubAppCommitStatusPublisher, error) {
	if cfg.Connections == nil || cfg.Registrations == nil || cfg.Secrets == nil || cfg.GitHubApps == nil {
		return nil, errors.New("github app commit status publisher requires SCM connection, repository registration, secret, and GitHub App dependencies")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = NewGitHubCommitStatusClient("", nil, "")
	}
	return &GitHubAppCommitStatusPublisher{connections: cfg.Connections, registrations: cfg.Registrations, secrets: cfg.Secrets, githubApps: cfg.GitHubApps, httpClient: cfg.HTTPClient}, nil
}

func (p *GitHubAppCommitStatusPublisher) PublishCommitStatus(ctx context.Context, req SCMCommitStatusPublishRequest) error {
	registrationID, connectionID, providerRepositoryID, identityErr := scmStatusRequestIdentity(req)
	if identityErr != nil {
		return identityErr
	}
	registration, err := p.registrations.GetByID(ctx, registrationID)
	if err != nil {
		return scmStatusIdentityError("github_status_registered_repository_unavailable", err)
	}
	if registration.ConnectionID != connectionID || registration.ProviderRepositoryID != providerRepositoryID {
		return scmStatusIdentityError("github_status_repository_identity_mismatch", errors.New("registered repository does not match delivery identity snapshot"))
	}
	detail, err := p.connections.GetByID(ctx, connectionID)
	if err != nil {
		return scmStatusIdentityError("github_status_connection_unavailable", err)
	}
	if !detail.Connection.Enabled {
		return scmStatusIdentityError("github_status_connection_disabled", errors.New("scm connection is disabled"))
	}
	if detail.Connection.Provider != domain.SCMProviderGitHub || detail.GitHubAppRegistration == nil || detail.GitHubAppInstallation == nil {
		return scmStatusIdentityError("github_status_connection_invalid", errors.New("github app connection configuration is invalid"))
	}
	app := detail.GitHubAppRegistration.Normalize()
	installation := detail.GitHubAppInstallation.Normalize()
	if app.PrivateKeySecretRef == "" || app.AppID == "" || installation.InstallationID == "" || detail.Connection.APIBaseURL != app.APIBaseURL {
		return scmStatusIdentityError("github_status_connection_invalid", errors.New("github app connection configuration is invalid"))
	}
	privateKey, err := p.secrets.Resolve(ctx, app.PrivateKeySecretRef)
	if err != nil || strings.TrimSpace(privateKey) == "" {
		return scmStatusIdentityError("github_status_private_key_unavailable", errors.New("github app private key could not be resolved"))
	}
	token, err := p.githubApps.GetInstallationToken(ctx, platformgithubapp.InstallationTokenRequest{AppRegistrationID: app.ID, AppID: app.AppID, InstallationID: installation.InstallationID, APIBaseURL: app.APIBaseURL, PrivateKeyPEM: privateKey})
	if err != nil {
		return classifyGitHubAppTokenError(err)
	}
	req.Provider = "github"
	req.RepositoryOwner = registration.Owner
	req.RepositoryName = registration.Name
	client := p.httpClient
	if strings.TrimSpace(app.APIBaseURL) != "" && strings.TrimRight(strings.TrimSpace(app.APIBaseURL), "/") != client.baseURL {
		client = NewGitHubCommitStatusClient(app.APIBaseURL, client.httpClient, "")
	}
	return client.PublishCommitStatusWithToken(ctx, req, token.Value)
}

func scmStatusRequestIdentity(req SCMCommitStatusPublishRequest) (string, string, string, error) {
	if req.RegisteredRepositoryID == nil || req.SCMConnectionID == nil || req.ProviderRepositoryID == nil {
		return "", "", "", scmStatusIdentityError("github_status_delivery_identity_missing", errors.New("repository identity snapshot is required"))
	}
	registrationID := strings.TrimSpace(*req.RegisteredRepositoryID)
	connectionID := strings.TrimSpace(*req.SCMConnectionID)
	providerRepositoryID := strings.TrimSpace(*req.ProviderRepositoryID)
	if registrationID == "" || connectionID == "" || providerRepositoryID == "" {
		return "", "", "", scmStatusIdentityError("github_status_delivery_identity_missing", errors.New("repository identity snapshot is incomplete"))
	}
	return registrationID, connectionID, providerRepositoryID, nil
}

func scmStatusIdentityError(reason string, err error) *GitHubCommitStatusError {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return &GitHubCommitStatusError{statusCode: http.StatusUnprocessableEntity, reason: reason, message: message}
}

func classifyGitHubAppTokenError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return err
	}

	statusErr := &GitHubCommitStatusError{statusCode: http.StatusBadGateway, retryable: true, reason: "github_status_token_exchange_failed", message: err.Error(), cause: err}
	switch {
	case errors.Is(err, platformgithubapp.ErrAuthentication):
		statusErr.statusCode = http.StatusUnauthorized
		statusErr.retryable = false
		statusErr.reason = "github_status_app_authentication_failed"
	case errors.Is(err, platformgithubapp.ErrInstallationUnavailable):
		statusErr.statusCode = http.StatusForbidden
		statusErr.retryable = false
		statusErr.reason = "github_status_installation_unavailable"
	case errors.Is(err, platformgithubapp.ErrPrivateKeyMissing), errors.Is(err, platformgithubapp.ErrPrivateKeyMalformed), errors.Is(err, platformgithubapp.ErrPrivateKeyNotRSA):
		statusErr.statusCode = http.StatusUnprocessableEntity
		statusErr.retryable = false
		statusErr.reason = "github_status_private_key_invalid"
	case errors.Is(err, platformgithubapp.ErrRepositoryInaccessible):
		statusErr.statusCode = http.StatusForbidden
		statusErr.retryable = false
		statusErr.reason = "github_status_repository_inaccessible"
	case errors.Is(err, platformgithubapp.ErrRateLimited):
		statusErr.statusCode = http.StatusTooManyRequests
		statusErr.reason = "github_status_rate_limited"
	case errors.Is(err, platformgithubapp.ErrProviderUnavailable):
		statusErr.statusCode = http.StatusServiceUnavailable
		statusErr.reason = "github_status_provider_unavailable"
	case errors.Is(err, platformgithubapp.ErrMalformedResponse):
		statusErr.reason = "github_status_provider_malformed_response"
	}
	return statusErr
}

var _ SCMCommitStatusPublisher = (*GitHubAppCommitStatusPublisher)(nil)
