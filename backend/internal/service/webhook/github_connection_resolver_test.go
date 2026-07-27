package webhook

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestGitHubConnectionResolver_ResolvesRegistrationSecretAndExactConnection(t *testing.T) {
	now := time.Now().UTC()
	connections := memoryrepo.NewSCMConnectionRepository()
	registrationA := testWebhookGitHubRegistration(now, "registration-a", "secret-a")
	registrationB := testWebhookGitHubRegistration(now, "registration-b", "secret-b")
	registrationB.AppID = "54321"
	registrationB.APIBaseURL = "https://ghe.example/api/v3"
	registrationB.WebBaseURL = "https://ghe.example"
	for _, registration := range []domain.GitHubAppRegistration{registrationA, registrationB} {
		if _, err := connections.CreateGitHubAppRegistration(context.Background(), registration); err != nil {
			t.Fatalf("create registration %q: %v", registration.ID, err)
		}
	}
	connectionA := testWebhookGitHubConnection(now, "connection-a", registrationA, "999")
	connectionB := testWebhookGitHubConnection(now, "connection-b", registrationB, "999")
	connectionB.Connection.APIBaseURL = registrationB.APIBaseURL
	connectionB.Connection.WebBaseURL = registrationB.WebBaseURL
	connectionB.Connection.DeploymentKind = domain.SCMDeploymentKindSelfHosted
	for _, detail := range []domain.SCMConnectionDetail{connectionA, connectionB} {
		if _, err := connections.CreateGitHubAppInstallationConnection(context.Background(), detail); err != nil {
			t.Fatalf("create connection %q: %v", detail.Connection.ID, err)
		}
	}

	resolver := NewGitHubConnectionResolver(connections, fakeWebhookSecretResolver{values: map[string]string{"secret-a": "webhook-a", "secret-b": "webhook-b"}})
	secret, secretErr := resolver.ResolveRegistrationSecret(context.Background(), "registration-a")
	if secretErr != nil || secret != "webhook-a" {
		t.Fatalf("expected registration-a secret, got secret=%q err=%v", secret, secretErr)
	}
	resolved, resolveErr := resolver.ResolveConnection(context.Background(), "registration-a", "999")
	if resolveErr != nil {
		t.Fatalf("resolve connection: %v", resolveErr)
	}
	if !resolved.Found || resolved.ConnectionID != "connection-a" || !resolved.Enabled {
		t.Fatalf("expected enabled connection-a, got %+v", resolved)
	}
}

func TestGitHubConnectionResolver_UnknownAndDisabledConnectionsAreNoOpCandidates(t *testing.T) {
	now := time.Now().UTC()
	connections := memoryrepo.NewSCMConnectionRepository()
	registration := testWebhookGitHubRegistration(now, "registration-a", "secret-a")
	if _, err := connections.CreateGitHubAppRegistration(context.Background(), registration); err != nil {
		t.Fatalf("create registration: %v", err)
	}
	detail := testWebhookGitHubConnection(now, "connection-a", registration, "999")
	if _, err := connections.CreateGitHubAppInstallationConnection(context.Background(), detail); err != nil {
		t.Fatalf("create connection: %v", err)
	}
	if _, err := connections.SetEnabled(context.Background(), "connection-a", false, now); err != nil {
		t.Fatalf("disable connection: %v", err)
	}

	resolver := NewGitHubConnectionResolver(connections, fakeWebhookSecretResolver{values: map[string]string{"secret-a": "webhook-a"}})
	disabled, disabledErr := resolver.ResolveConnection(context.Background(), "registration-a", "999")
	if disabledErr != nil || !disabled.Found || disabled.Enabled {
		t.Fatalf("expected disabled match, got result=%+v err=%v", disabled, disabledErr)
	}
	unknown, unknownErr := resolver.ResolveConnection(context.Background(), "registration-a", "missing")
	if unknownErr != nil || unknown.Found {
		t.Fatalf("expected unknown connection no-op candidate, got result=%+v err=%v", unknown, unknownErr)
	}
}

func TestGitHubConnectionResolver_HidesSecretResolutionFailure(t *testing.T) {
	connections := &fakeWebhookConnectionRepository{registration: domain.GitHubAppRegistration{ID: "registration-a", WebhookSecretRef: "secret-a"}}
	resolver := NewGitHubConnectionResolver(connections, fakeWebhookSecretResolver{err: errors.New("secret-a is missing")})
	_, err := resolver.ResolveRegistrationSecret(context.Background(), "registration-a")
	if !errors.Is(err, ErrGitHubWebhookSecretUnavailable) || err.Error() != ErrGitHubWebhookSecretUnavailable.Error() {
		t.Fatalf("expected masked secret resolution error, got %v", err)
	}
	_, err = resolver.ResolveRegistrationSecret(context.Background(), "missing")
	if !errors.Is(err, ErrGitHubWebhookRegistrationNotFound) {
		t.Fatalf("expected registration not found, got %v", err)
	}
}

func TestGitHubConnectionResolver_ResolveConnectionRejectsIncompatibleDetails(t *testing.T) {
	now := time.Now().UTC()
	registration := testWebhookGitHubRegistration(now, "registration-a", "secret-a")
	detail := testWebhookGitHubConnection(now, "connection-a", registration, "999")
	detail.GitHubAppInstallation.InstallationID = "different"
	resolver := NewGitHubConnectionResolver(&fakeWebhookConnectionRepository{registration: registration, detail: detail}, fakeWebhookSecretResolver{})

	_, resolveErr := resolver.ResolveConnection(context.Background(), " registration-a ", " 999 ")
	if !errors.Is(resolveErr, ErrGitHubWebhookConnectionIncompatible) {
		t.Fatalf("expected incompatible connection error, got %v", resolveErr)
	}
}

func TestGitHubConnectionResolver_ResolveConnectionPropagatesRepositoryFailures(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	resolver := NewGitHubConnectionResolver(&fakeWebhookConnectionRepository{connectionErr: repositoryErr}, fakeWebhookSecretResolver{})

	_, resolveErr := resolver.ResolveConnection(context.Background(), "registration-a", "999")
	if !errors.Is(resolveErr, repositoryErr) {
		t.Fatalf("expected repository error %v, got %v", repositoryErr, resolveErr)
	}
}

func TestGitHubConnectionResolver_RequiresRegistrationAndSecretConfiguration(t *testing.T) {
	if _, err := (*GitHubConnectionResolver)(nil).ResolveRegistrationSecret(context.Background(), "registration-a"); !errors.Is(err, ErrGitHubWebhookRegistrationNotFound) {
		t.Fatalf("expected nil resolver registration error, got %v", err)
	}
	if _, err := (*GitHubConnectionResolver)(nil).ResolveConnection(context.Background(), "registration-a", "999"); !errors.Is(err, ErrGitHubWebhookConnectionIncompatible) {
		t.Fatalf("expected nil resolver connection error, got %v", err)
	}

	resolver := NewGitHubConnectionResolver(&fakeWebhookConnectionRepository{registration: domain.GitHubAppRegistration{ID: "registration-a"}}, fakeWebhookSecretResolver{})
	if _, err := resolver.ResolveRegistrationSecret(context.Background(), "registration-a"); !errors.Is(err, ErrGitHubWebhookSecretUnavailable) {
		t.Fatalf("expected missing secret reference error, got %v", err)
	}
}

type fakeWebhookSecretResolver struct {
	values map[string]string
	err    error
}

func (r fakeWebhookSecretResolver) Resolve(_ context.Context, ref string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.values[ref], nil
}

type fakeWebhookConnectionRepository struct {
	registration  domain.GitHubAppRegistration
	detail        domain.SCMConnectionDetail
	connectionErr error
}

func (r *fakeWebhookConnectionRepository) GetGitHubAppRegistrationByID(_ context.Context, id string) (domain.GitHubAppRegistration, error) {
	if id != r.registration.ID {
		return domain.GitHubAppRegistration{}, repository.ErrSCMGitHubAppRegistrationNotFound
	}
	return r.registration, nil
}

func (r *fakeWebhookConnectionRepository) GetGitHubAppInstallationConnection(_ context.Context, _ string, _ string) (domain.SCMConnectionDetail, error) {
	if r.connectionErr != nil {
		return domain.SCMConnectionDetail{}, r.connectionErr
	}
	if r.detail.Connection.ID != "" {
		return r.detail, nil
	}
	return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
}

func testWebhookGitHubRegistration(now time.Time, id string, secretRef string) domain.GitHubAppRegistration {
	return domain.GitHubAppRegistration{ID: id, AppID: "12345", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "private-key", WebhookSecretRef: secretRef, CreatedAt: now, UpdatedAt: now}
}

func testWebhookGitHubConnection(now time.Time, connectionID string, registration domain.GitHubAppRegistration, installationID string) domain.SCMConnectionDetail {
	return domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: connectionID, Provider: domain.SCMProviderGitHub, DisplayName: connectionID, DeploymentKind: domain.SCMDeploymentKindCloud, APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &registration,
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: connectionID, AppRegistrationID: registration.ID, InstallationID: installationID, AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now},
	}
}
