package webhook

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrGitHubWebhookRegistrationNotFound = errors.New("github webhook registration not found")
var ErrGitHubWebhookSecretUnavailable = errors.New("github webhook secret is unavailable")
var ErrGitHubWebhookConnectionIncompatible = errors.New("github webhook connection is incompatible")

type GitHubWebhookConnectionResolver interface {
	ResolveRegistrationSecret(ctx context.Context, registrationID string) (string, error)
	ResolveConnection(ctx context.Context, registrationID string, installationID string) (GitHubWebhookConnectionResolution, error)
}

type GitHubWebhookConnectionResolution struct {
	ConnectionID string
	Found        bool
	Enabled      bool
}

type githubWebhookConnectionRepository interface {
	GetGitHubAppRegistrationByID(ctx context.Context, id string) (domain.GitHubAppRegistration, error)
	GetGitHubAppInstallationConnection(ctx context.Context, registrationID string, installationID string) (domain.SCMConnectionDetail, error)
}

type githubWebhookSecretResolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

type GitHubConnectionResolver struct {
	connections githubWebhookConnectionRepository
	secrets     githubWebhookSecretResolver
}

func NewGitHubConnectionResolver(connections githubWebhookConnectionRepository, secrets githubWebhookSecretResolver) *GitHubConnectionResolver {
	return &GitHubConnectionResolver{connections: connections, secrets: secrets}
}

func (r *GitHubConnectionResolver) ResolveRegistrationSecret(ctx context.Context, registrationID string) (string, error) {
	if r == nil || r.connections == nil {
		return "", ErrGitHubWebhookRegistrationNotFound
	}
	registration, err := r.connections.GetGitHubAppRegistrationByID(ctx, strings.TrimSpace(registrationID))
	if err != nil {
		if errors.Is(err, repository.ErrSCMGitHubAppRegistrationNotFound) {
			return "", ErrGitHubWebhookRegistrationNotFound
		}
		return "", err
	}
	if r.secrets == nil || strings.TrimSpace(registration.WebhookSecretRef) == "" {
		return "", ErrGitHubWebhookSecretUnavailable
	}
	secret, err := r.secrets.Resolve(ctx, registration.WebhookSecretRef)
	if err != nil || strings.TrimSpace(secret) == "" {
		return "", ErrGitHubWebhookSecretUnavailable
	}
	return secret, nil
}

func (r *GitHubConnectionResolver) ResolveConnection(ctx context.Context, registrationID string, installationID string) (GitHubWebhookConnectionResolution, error) {
	if r == nil || r.connections == nil {
		return GitHubWebhookConnectionResolution{}, ErrGitHubWebhookConnectionIncompatible
	}
	registrationID = strings.TrimSpace(registrationID)
	installationID = strings.TrimSpace(installationID)
	detail, err := r.connections.GetGitHubAppInstallationConnection(ctx, registrationID, installationID)
	if err != nil {
		if errors.Is(err, repository.ErrSCMConnectionNotFound) {
			return GitHubWebhookConnectionResolution{}, nil
		}
		return GitHubWebhookConnectionResolution{}, err
	}
	if detail.Connection.Provider != domain.SCMProviderGitHub || detail.GitHubAppRegistration == nil || detail.GitHubAppInstallation == nil || detail.GitHubAppRegistration.ID != registrationID || detail.GitHubAppInstallation.AppRegistrationID != registrationID || detail.GitHubAppInstallation.InstallationID != installationID {
		return GitHubWebhookConnectionResolution{}, ErrGitHubWebhookConnectionIncompatible
	}
	return GitHubWebhookConnectionResolution{ConnectionID: detail.Connection.ID, Found: true, Enabled: detail.Connection.Enabled}, nil
}
